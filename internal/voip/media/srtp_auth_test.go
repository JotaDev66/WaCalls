package media

import (
	"bytes"
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
