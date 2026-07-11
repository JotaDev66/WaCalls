package media

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"wacalls/internal/voip/core"
)

func authTestPair(t *testing.T) (*SrtpSession, *SrtpSession) {
	t.Helper()
	callKey := bytes.Repeat([]byte{0x11}, 32)
	sendKM, err := DerivePerJidSrtpKey(callKey, "self:0@lid")
	if err != nil {
		t.Fatal(err)
	}
	recvKM, err := DerivePerJidSrtpKey(callKey, "peer:0@lid")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSrtpSession(sendKM, recvKM, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewSrtpSession(recvKM, sendKM, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	return sender, receiver
}

func assertSrtpErr(t *testing.T, err error, want SrtpErrorType) {
	t.Helper()
	var se *SrtpError
	if !errors.As(err, &se) {
		t.Fatalf("want *SrtpError type %q, got %v", want, err)
	}
	if se.Type != want {
		t.Fatalf("want error type %q, got %q (%v)", want, se.Type, err)
	}
}

func TestSrtpAuthTagKat(t *testing.T) {
	callKey, err := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	km, err := DerivePerJidSrtpKey(callKey, "222222222222222:0@lid")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := NewSrtpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(ctx.authKey); got != "8eff4bb04971d92512b034ce0ebc466059bfc6ea" {
		t.Fatalf("authKey mismatch vs whatsapp-rust kat: %s", got)
	}
	samplePacket, err := hex.DecodeString("90780007000003c012345678deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(ctx.computeAuthTag(samplePacket, 0, 4)); got != "53fada83" {
		t.Fatalf("auth tag mismatch vs whatsapp-rust kat: %s", got)
	}
}

func TestUnprotectRoundtripMultiPacket(t *testing.T) {
	sender, receiver := authTestPair(t)
	sess := NewWhatsAppOpusSession(0xAABBCCDD)
	for i := range 5 {
		payload := bytes.Repeat([]byte{byte(0x40 + i)}, 40)
		wire, err := sender.Protect(sess.CreatePacket(payload, i == 0))
		if err != nil {
			t.Fatal(err)
		}
		got, err := receiver.Unprotect(wire)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !bytes.Equal(got.Payload, payload) {
			t.Fatalf("packet %d payload mismatch", i)
		}
	}
}

func TestUnprotectRejectsCorruptedTag(t *testing.T) {
	sender, receiver := authTestPair(t)
	sess := NewWhatsAppOpusSession(0xAABBCCDD)
	wire, err := sender.Protect(sess.CreatePacket(bytes.Repeat([]byte{0x42}, 40), true))
	if err != nil {
		t.Fatal(err)
	}
	wire[len(wire)-1] ^= 0x01
	_, err = receiver.Unprotect(wire)
	assertSrtpErr(t, err, SrtpErrAuthFailed)
}

func TestUnprotectRejectsTruncatedTag(t *testing.T) {
	sender, receiver := authTestPair(t)
	sess := NewWhatsAppOpusSession(0xAABBCCDD)
	wire, err := sender.Protect(sess.CreatePacket(bytes.Repeat([]byte{0x42}, 40), true))
	if err != nil {
		t.Fatal(err)
	}
	_, err = receiver.Unprotect(wire[:len(wire)-2])
	assertSrtpErr(t, err, SrtpErrAuthFailed)

	_, err = receiver.Unprotect(wire[:12+core.SRTPRecvAuthTagLen])
	assertSrtpErr(t, err, SrtpErrPacketTooShort)
}

func TestUnprotectRejectsTamperedPayload(t *testing.T) {
	sender, receiver := authTestPair(t)
	sess := NewWhatsAppOpusSession(0xAABBCCDD)
	wire, err := sender.Protect(sess.CreatePacket(bytes.Repeat([]byte{0x42}, 40), true))
	if err != nil {
		t.Fatal(err)
	}
	wire[14] ^= 0xFF
	_, err = receiver.Unprotect(wire)
	assertSrtpErr(t, err, SrtpErrAuthFailed)
}

func TestUnprotectForgedPacketDoesNotDesyncRoc(t *testing.T) {
	sender, receiver := authTestPair(t)
	sess := NewWhatsAppOpusSession(0x0BADF00D)
	payload := bytes.Repeat([]byte{0x50}, 40)

	first, err := sender.Protect(sess.CreatePacket(payload, true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Unprotect(first); err != nil {
		t.Fatal(err)
	}
	baseSeq := binary.BigEndian.Uint16(first[2:4])

	forged, err := sender.Protect(sess.CreatePacket(payload, false))
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint16(forged[2:4], baseSeq+0x4000)
	_, err = receiver.Unprotect(forged)
	assertSrtpErr(t, err, SrtpErrAuthFailed)

	next, err := sender.Protect(sess.CreatePacket(payload, false))
	if err != nil {
		t.Fatal(err)
	}
	got, err := receiver.Unprotect(next)
	if err != nil {
		t.Fatalf("legit frame after forged packet: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Fatal("payload mismatch after forged packet")
	}
}

func TestUnprotectRoundtripAcrossSeqWrap(t *testing.T) {
	sender, receiver := authTestPair(t)
	seqs := []uint16{0xFFFE, 0xFFFF, 0x0000, 0x0001}
	payloads := make([][]byte, len(seqs))
	wires := make([][]byte, len(seqs))
	for i, seq := range seqs {
		payloads[i] = bytes.Repeat([]byte{byte(i)*37 + 1}, 40)
		pkt := &RtpPacket{
			Header:  NewRtpHeader(core.PayloadTypeWhatsAppOpus, seq, uint32(seq), 0x57410001),
			Payload: payloads[i],
		}
		wire, err := sender.Protect(pkt)
		if err != nil {
			t.Fatal(err)
		}
		wires[i] = wire
	}

	for _, i := range []int{0, 1, 3, 2} {
		got, err := receiver.Unprotect(wires[i])
		if err != nil {
			t.Fatalf("seq %#04x: %v", seqs[i], err)
		}
		if !bytes.Equal(got.Payload, payloads[i]) {
			t.Fatalf("seq %#04x payload mismatch", seqs[i])
		}
	}

	got, err := receiver.Unprotect(wires[1])
	if err != nil {
		t.Fatalf("late pre-wrap packet: %v", err)
	}
	if !bytes.Equal(got.Payload, payloads[1]) {
		t.Fatal("late pre-wrap packet must decrypt under roc-1")
	}
}

func TestEstimateRocDoesNotAdvanceWithoutCommit(t *testing.T) {
	km, err := DerivePerJidSrtpKey(bytes.Repeat([]byte{0x22}, 32), "peer:0@lid")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := NewSrtpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}

	ctx.commitRoc(ctx.estimateRoc(0x7FFE), 0x7FFE)
	if ctx.roc != 0 {
		t.Fatalf("roc after seed: %d", ctx.roc)
	}

	_ = ctx.estimateRoc(0xFFFE)
	_ = ctx.estimateRoc(0x7FFD)
	if ctx.roc != 0 {
		t.Fatalf("estimate alone advanced roc: %d", ctx.roc)
	}
	if got := ctx.estimateRoc(0x7FFF); got != 0 {
		t.Fatalf("legit in-window seq estimated roc %d, want 0", got)
	}

	ctx.commitRoc(ctx.estimateRoc(0xFFFE), 0xFFFE)
	ctx.commitRoc(ctx.estimateRoc(0x7FFD), 0x7FFD)
	if ctx.roc != 1 {
		t.Fatalf("committed staircase must advance roc to 1, got %d", ctx.roc)
	}
}
