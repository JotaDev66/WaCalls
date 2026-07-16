package engine

import (
	"bytes"
	"testing"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

func testKM(seed byte) core.SrtpKeyingMaterial {
	mk := make([]byte, 16)
	ms := make([]byte, 14)
	for i := range mk {
		mk[i] = seed + byte(i)
	}
	for i := range ms {
		ms[i] = seed*2 + byte(i)
	}
	return core.SrtpKeyingMaterial{MasterKey: mk, MasterSalt: ms}
}

func rtpPkt(ssrc uint32, seq uint16, payload []byte) *media.RtpPacket {
	return &media.RtpPacket{
		Header:  media.NewRtpHeader(core.PayloadTypeWhatsAppOpus, seq, uint32(seq), ssrc),
		Payload: payload,
	}
}

func mustProtect(t *testing.T, m *SrtpManager, pkt *media.RtpPacket) []byte {
	t.Helper()
	wire, err := m.Protect(pkt)
	if err != nil {
		t.Fatalf("Protect: %v", err)
	}
	return wire
}

func TestSrtpManager_Roundtrip(t *testing.T) {
	k1, k2 := testKM(1), testKM(9)
	sender := NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	receiver := NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)

	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	got, err := receiver.Unprotect(mustProtect(t, sender, rtpPkt(7, 1, payload)))
	if err != nil {
		t.Fatalf("Unprotect: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Fatalf("roundtrip mismatch: got %x want %x", got.Payload, payload)
	}
}

func TestSrtpManager_RejectsDuplicatePacket(t *testing.T) {
	k1, k2 := testKM(1), testKM(9)
	sender := NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	receiver := NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)

	wire := mustProtect(t, sender, rtpPkt(7, 42, []byte{0xAA, 0xBB}))
	if _, err := receiver.Unprotect(wire); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if _, err := receiver.Unprotect(wire); err == nil {
		t.Fatal("duplicate packet must not be decoded twice")
	}
}

func TestSrtpManager_PerSsrcRocIsolation(t *testing.T) {
	k1, k2 := testKM(1), testKM(9)
	sender := NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	receiver := NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)

	const a, b = uint32(1111), uint32(2222)
	payA1 := []byte{0xA1, 0xA1, 0xA1, 0xA1}
	payB := []byte{0xB0, 0xB0, 0xB0, 0xB0}
	payA2 := []byte{0xA2, 0xA2, 0xA2, 0xA2}

	wireA1 := mustProtect(t, sender, rtpPkt(a, 100, payA1))
	wireB := mustProtect(t, sender, rtpPkt(b, 60000, payB))
	wireA2 := mustProtect(t, sender, rtpPkt(a, 101, payA2))

	gotA1, err := receiver.Unprotect(wireA1)
	if err != nil || !bytes.Equal(gotA1.Payload, payA1) {
		t.Fatalf("A1: err=%v payload=%x", err, gotA1.Payload)
	}
	gotB, err := receiver.Unprotect(wireB)
	if err != nil || !bytes.Equal(gotB.Payload, payB) {
		t.Fatalf("B: err=%v payload=%x", err, gotB.Payload)
	}
	gotA2, err := receiver.Unprotect(wireA2)
	if err != nil || !bytes.Equal(gotA2.Payload, payA2) {
		t.Fatalf("A2 (ROC leaked across SSRC): err=%v payload=%x", err, gotA2.Payload)
	}
}

func TestSrtpManager_RekeyRecv(t *testing.T) {
	k1, k2, k3 := testKM(1), testKM(9), testKM(21)
	senderOld := NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	receiver := NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)

	const ssrc = uint32(4242)
	old := []byte{0x11, 0x22, 0x33, 0x44}
	got, err := receiver.Unprotect(mustProtect(t, senderOld, rtpPkt(ssrc, 5, old)))
	if err != nil || !bytes.Equal(got.Payload, old) {
		t.Fatalf("pre-rekey: err=%v payload=%x", err, got.Payload)
	}

	receiver.RekeyRecv(k3)
	senderNew := NewSrtpManager(k3, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	fresh := []byte{0x55, 0x66, 0x77, 0x88}
	got2, err := receiver.Unprotect(mustProtect(t, senderNew, rtpPkt(ssrc, 6, fresh)))
	if err != nil || !bytes.Equal(got2.Payload, fresh) {
		t.Fatalf("post-rekey: err=%v payload=%x", err, got2.Payload)
	}
}
