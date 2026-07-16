package media

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"wacalls/internal/voip/core"
)

// KAT vectors captured from a real WhatsApp 1:1 call (official client) and decrypted offline:
// given the E2E SRTCP keying, protecting the plaintext RTCP compound must reproduce the exact
// bytes the official client put on the wire. The master keying is opaque HKDF output (derived
// from the call key + participant LID), not the call key itself.
func TestSrtcpProtectKAT(t *testing.T) {
	dec := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatalf("bad hex %q: %v", s, err)
		}
		return b
	}
	keying := core.SrtpKeyingMaterial{
		MasterKey:  dec("cb833e18a62af9c7dfc264279eda9c07"),
		MasterSalt: dec("cb01d8ce63805a98737cec576ce8"),
	}
	ctx, err := NewSrtcpContext(keying)
	if err != nil {
		t.Fatalf("NewSrtcpContext: %v", err)
	}

	cases := []struct {
		name      string
		index     uint32
		plaintext string
		wire      string
	}{
		{"compact209", 3, "81d100017c8657c9", "81d100017c8657c980000003af1a0b0fb99d9c70498e"},
		{"compact208", 2, "81d000027c8657c900004380", "81d000027c8657c98e5d9cc680000002d956b30f1485bd414b2d"},
		{
			"sr_sdes_compound", 4,
			"81c800127c8657c9edffd5ba8112dc7200024e490000000c000000225dd9e96f000000000000000a00000000d5b873e200001f0000000099000047680000000000000000000000000000000081ca00077c8657c90112343666636240706a6137613564322e6f726700000000",
			"81c800127c8657c9198d49b01f4cb5c9c6b526762b3a63ba2d93d57a7e2eb96c28a23e03981675e5ce0e91ebf2bb9e21a6028d6619f781a8c718efd16b53c318b1ff571c7dad8bc59f7ca132fd566d7eae57945776781ee779ee00ccc890854db92406cb5ca86a191a99800c80000004431b63f782f5d955b787",
		},
	}
	for _, tc := range cases {
		got, err := ctx.Protect(dec(tc.plaintext), tc.index)
		if err != nil {
			t.Errorf("%s: protect error: %v", tc.name, err)
			continue
		}
		if hex.EncodeToString(got) != tc.wire {
			t.Errorf("%s: protect mismatch\n got  %s\n want %s", tc.name, hex.EncodeToString(got), tc.wire)
		}
	}
}

// TestSrtcpUnprotectKAT is the inverse of TestSrtcpProtectKAT: decoding the exact SRTCP bytes the
// official client put on the wire, with the same E2E keying, must recover the plaintext compound.
func TestSrtcpUnprotectKAT(t *testing.T) {
	dec := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatalf("bad hex %q: %v", s, err)
		}
		return b
	}
	keying := core.SrtpKeyingMaterial{
		MasterKey:  dec("cb833e18a62af9c7dfc264279eda9c07"),
		MasterSalt: dec("cb01d8ce63805a98737cec576ce8"),
	}
	ctx, err := NewSrtcpContext(keying)
	if err != nil {
		t.Fatalf("NewSrtcpContext: %v", err)
	}
	cases := []struct {
		name      string
		plaintext string
		wire      string
	}{
		{"compact209", "81d100017c8657c9", "81d100017c8657c980000003af1a0b0fb99d9c70498e"},
		{"compact208", "81d000027c8657c900004380", "81d000027c8657c98e5d9cc680000002d956b30f1485bd414b2d"},
		{
			"sr_sdes_compound",
			"81c800127c8657c9edffd5ba8112dc7200024e490000000c000000225dd9e96f000000000000000a00000000d5b873e200001f0000000099000047680000000000000000000000000000000081ca00077c8657c90112343666636240706a6137613564322e6f726700000000",
			"81c800127c8657c9198d49b01f4cb5c9c6b526762b3a63ba2d93d57a7e2eb96c28a23e03981675e5ce0e91ebf2bb9e21a6028d6619f781a8c718efd16b53c318b1ff571c7dad8bc59f7ca132fd566d7eae57945776781ee779ee00ccc890854db92406cb5ca86a191a99800c80000004431b63f782f5d955b787",
		},
	}
	for _, tc := range cases {
		got, err := ctx.Unprotect(dec(tc.wire))
		if err != nil {
			t.Errorf("%s: unprotect error: %v", tc.name, err)
			continue
		}
		if hex.EncodeToString(got) != tc.plaintext {
			t.Errorf("%s: unprotect mismatch\n got  %s\n want %s", tc.name, hex.EncodeToString(got), tc.plaintext)
		}
	}
}

func TestSrtcpRoundtrip(t *testing.T) {
	keying := core.SrtpKeyingMaterial{MasterKey: bytes.Repeat([]byte{0x11}, 16), MasterSalt: bytes.Repeat([]byte{0x22}, 14)}
	ctx, err := NewSrtcpContext(keying)
	if err != nil {
		t.Fatal(err)
	}
	rtcp := BuildCompact208(0x7c8657c9, 0x12345678)
	protected, err := ctx.Protect(rtcp[:], 42)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ctx.Unprotect(protected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, rtcp[:]) {
		t.Fatalf("roundtrip mismatch: got %x want %x", plain, rtcp[:])
	}
}

func TestSrtcpUnprotectAuthFail(t *testing.T) {
	keying := core.SrtpKeyingMaterial{MasterKey: bytes.Repeat([]byte{0x11}, 16), MasterSalt: bytes.Repeat([]byte{0x22}, 14)}
	ctx, _ := NewSrtcpContext(keying)
	rtcp := BuildCompact208(1, 2)
	protected, _ := ctx.Protect(rtcp[:], 1)
	protected[len(protected)-1] ^= 0xff
	_, err := ctx.Unprotect(protected)
	var se *SrtpError
	if !errors.As(err, &se) || se.Type != SrtpErrAuthFailed {
		t.Fatalf("want auth failed, got %v", err)
	}
}

func TestSrtcpUnprotectTooShort(t *testing.T) {
	ctx, _ := NewSrtcpContext(core.SrtpKeyingMaterial{MasterKey: make([]byte, 16), MasterSalt: make([]byte, 14)})
	_, err := ctx.Unprotect(make([]byte, 21))
	var se *SrtpError
	if !errors.As(err, &se) || se.Type != SrtpErrPacketTooShort {
		t.Fatalf("want too short, got %v", err)
	}
}
