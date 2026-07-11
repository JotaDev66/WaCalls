package media

import (
	"bytes"
	"errors"
	"testing"

	"wacalls/internal/voip/core"
)

func securityTestKM(seed byte) core.SrtpKeyingMaterial {
	key := make([]byte, 16)
	salt := make([]byte, 14)
	for i := range key {
		key[i] = seed + byte(i)
	}
	for i := range salt {
		salt[i] = seed*3 + byte(i)
	}
	return core.SrtpKeyingMaterial{MasterKey: key, MasterSalt: salt}
}

func securityPacket(seq uint16, payload []byte) *RtpPacket {
	return &RtpPacket{
		Header:  NewRtpHeader(core.PayloadTypeWhatsAppOpus, seq, uint32(seq)*960, 0xAABBCCDD),
		Payload: payload,
	}
}

func securityContexts(t *testing.T) (*SrtpContext, *SrtpContext) {
	t.Helper()
	km := securityTestKM(7)
	sender, err := NewSrtpContext(km, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewSrtpContext(km, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	return sender, receiver
}

func securityProtect(t *testing.T, sender *SrtpContext, seq uint16) []byte {
	t.Helper()
	payload := []byte{byte(seq), byte(seq >> 8), 0xCA, 0xFE}
	wire, err := sender.Protect(securityPacket(seq, payload))
	if err != nil {
		t.Fatalf("Protect(%d): %v", seq, err)
	}
	return wire
}

func requireSrtpError(t *testing.T, err error, want SrtpErrorType) {
	t.Helper()
	var srtpErr *SrtpError
	if !errors.As(err, &srtpErr) {
		t.Fatalf("expected SrtpError %q, got %T: %v", want, err, err)
	}
	if srtpErr.Type != want {
		t.Fatalf("expected SrtpError %q, got %q", want, srtpErr.Type)
	}
}

func TestSrtpRejectsTamperedHeaderPayloadAndTag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset func([]byte) int
	}{
		{name: "header", offset: func(_ []byte) int { return 4 }},
		{name: "payload", offset: func(_ []byte) int { return 12 }},
		{name: "tag", offset: func(wire []byte) int { return len(wire) - 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender, receiver := securityContexts(t)
			wire := securityProtect(t, sender, 10)
			wire[tc.offset(wire)] ^= 0x80
			_, err := receiver.Unprotect(wire)
			requireSrtpError(t, err, SrtpErrAuthFailed)
		})
	}
}

func TestSrtpAuthFailureDoesNotAdvanceReceiveState(t *testing.T) {
	sender, receiver := securityContexts(t)
	bad := securityProtect(t, sender, 65535)
	bad[len(bad)-1] ^= 0x01
	_, err := receiver.Unprotect(bad)
	requireSrtpError(t, err, SrtpErrAuthFailed)

	// A failed high sequence number must not initialize the context or alter ROC.
	freshSender, _ := securityContexts(t)
	good := securityProtect(t, freshSender, 1)
	got, err := receiver.Unprotect(good)
	if err != nil {
		t.Fatalf("valid packet after auth failure: %v", err)
	}
	if !bytes.Equal(got.Payload, []byte{1, 0, 0xCA, 0xFE}) {
		t.Fatalf("unexpected payload: %x", got.Payload)
	}
}

func TestSrtpRejectsDuplicateReplay(t *testing.T) {
	sender, receiver := securityContexts(t)
	wire := securityProtect(t, sender, 42)
	if _, err := receiver.Unprotect(wire); err != nil {
		t.Fatalf("first packet: %v", err)
	}
	_, err := receiver.Unprotect(wire)
	requireSrtpError(t, err, SrtpErrReplay)
}

func TestSrtpAcceptsOutOfOrderPacketOnce(t *testing.T) {
	sender, receiver := securityContexts(t)
	w100 := securityProtect(t, sender, 100)
	w102 := securityProtect(t, sender, 102)
	w101 := securityProtect(t, sender, 101)

	for _, wire := range [][]byte{w100, w102, w101} {
		if _, err := receiver.Unprotect(wire); err != nil {
			t.Fatalf("out-of-order packet rejected: %v", err)
		}
	}
	_, err := receiver.Unprotect(w101)
	requireSrtpError(t, err, SrtpErrReplay)
}

func TestSrtpRejectsPacketOutsideReplayWindow(t *testing.T) {
	sender, receiver := securityContexts(t)
	old := securityProtect(t, sender, 1)
	newest := securityProtect(t, sender, 70)
	if _, err := receiver.Unprotect(old); err != nil {
		t.Fatalf("old first delivery: %v", err)
	}
	if _, err := receiver.Unprotect(newest); err != nil {
		t.Fatalf("newest delivery: %v", err)
	}
	_, err := receiver.Unprotect(old)
	requireSrtpError(t, err, SrtpErrReplay)
}

func TestSrtpRolloverAndLatePreviousRoc(t *testing.T) {
	sender, receiver := securityContexts(t)
	late := securityProtect(t, sender, 65530)
	wires := [][]byte{
		securityProtect(t, sender, 65534),
		securityProtect(t, sender, 65535),
		securityProtect(t, sender, 0),
		securityProtect(t, sender, 1),
	}
	for _, wire := range wires {
		if _, err := receiver.Unprotect(wire); err != nil {
			t.Fatalf("rollover packet rejected: %v", err)
		}
	}
	if _, err := receiver.Unprotect(late); err != nil {
		t.Fatalf("late previous-ROC packet rejected: %v", err)
	}
}

func TestNewSrtpContextValidatesKeyMaterialAndTagLength(t *testing.T) {
	km := securityTestKM(1)
	badKey := km
	badKey.MasterKey = badKey.MasterKey[:15]
	if _, err := NewSrtpContext(badKey, 10); err == nil {
		t.Fatal("expected invalid master-key length error")
	}
	badSalt := km
	badSalt.MasterSalt = badSalt.MasterSalt[:13]
	if _, err := NewSrtpContext(badSalt, 10); err == nil {
		t.Fatal("expected invalid master-salt length error")
	}
	if _, err := NewSrtpContext(km, 21); err == nil {
		t.Fatal("expected invalid auth-tag length error")
	}
}
