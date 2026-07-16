package media

import (
	"bytes"
	"testing"

	"wacalls/internal/voip/core"
)

func replayPair(t *testing.T) (*SrtpContext, *SrtpContext) {
	t.Helper()
	km, err := DerivePerJidSrtpKey(bytes.Repeat([]byte{0x31}, 32), "peer:0@lid")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSrtpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewSrtpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	return sender, receiver
}

func replayWire(t *testing.T, sender *SrtpContext, seq uint16) []byte {
	t.Helper()
	pkt := &RtpPacket{
		Header:  NewRtpHeader(core.PayloadTypeWhatsAppOpus, seq, uint32(seq)*160, 0x57414301),
		Payload: bytes.Repeat([]byte{byte(seq)}, 40),
	}
	wire, err := sender.Protect(pkt)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestUnprotectRejectsImmediateDuplicate(t *testing.T) {
	sender, receiver := replayPair(t)
	wire := replayWire(t, sender, 100)
	if _, err := receiver.Unprotect(wire); err != nil {
		t.Fatal(err)
	}
	_, err := receiver.Unprotect(wire)
	assertSrtpErr(t, err, SrtpErrReplay)
}

func TestUnprotectAcceptsReorderWithinWindow(t *testing.T) {
	sender, receiver := replayPair(t)
	w10 := replayWire(t, sender, 10)
	w11 := replayWire(t, sender, 11)
	w12 := replayWire(t, sender, 12)
	for _, wire := range [][]byte{w10, w12, w11} {
		if _, err := receiver.Unprotect(wire); err != nil {
			t.Fatalf("reordered delivery must pass: %v", err)
		}
	}
	_, err := receiver.Unprotect(w12)
	assertSrtpErr(t, err, SrtpErrReplay)
}

func TestUnprotectRejectsTooOldPacket(t *testing.T) {
	sender, receiver := replayPair(t)
	old := replayWire(t, sender, 35)
	for seq := uint16(36); seq <= 100; seq++ {
		replayWire(t, sender, seq)
	}
	head := replayWire(t, sender, 101)
	if _, err := receiver.Unprotect(head); err != nil {
		t.Fatal(err)
	}
	_, err := receiver.Unprotect(old)
	assertSrtpErr(t, err, SrtpErrReplay)
}

func TestForgedPacketDoesNotAdvanceReplayWindow(t *testing.T) {
	sender, receiver := replayPair(t)
	if _, err := receiver.Unprotect(replayWire(t, sender, 10)); err != nil {
		t.Fatal(err)
	}
	genuine := replayWire(t, sender, 11)
	forged := append([]byte(nil), genuine...)
	forged[len(forged)-1] ^= 0xFF
	_, err := receiver.Unprotect(forged)
	assertSrtpErr(t, err, SrtpErrAuthFailed)
	if _, err := receiver.Unprotect(genuine); err != nil {
		t.Fatalf("genuine packet after forged twin must pass: %v", err)
	}
}
