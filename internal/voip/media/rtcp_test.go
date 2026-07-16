package media

import (
	"encoding/hex"
	"testing"
)

func TestBuildCompact208KAT(t *testing.T) {
	got := BuildCompact208(0x12345678, 0x9abcdef0)
	if h := hex.EncodeToString(got[:]); h != "81d00002123456789abcdef0" {
		t.Fatalf("208 got %s", h)
	}
}

func TestBuildCompact209KAT(t *testing.T) {
	got := BuildCompact209(0x12345678)
	if h := hex.EncodeToString(got[:]); h != "81d1000112345678" {
		t.Fatalf("209 got %s", h)
	}
}

func TestBuildSenderReportKAT(t *testing.T) {
	got := BuildSenderReport(0x12345678, RTCPSenderStats{PacketsSent: 5, OctetsSent: 600, RtpTimestamp: 1600}, 1718000000000)
	if h := hex.EncodeToString(got[:]); h != "80c8000612345678ea11180000000000000006400000000500000258" {
		t.Fatalf("SR got %s", h)
	}
}

func TestParseRTCPSenderSSRC(t *testing.T) {
	sr := BuildSenderReport(0x12345678, RTCPSenderStats{}, 0)
	ssrc, ok := ParseRTCPSenderSSRC(sr[:])
	if !ok || ssrc != 0x12345678 {
		t.Fatalf("got %x ok=%v", ssrc, ok)
	}
	if _, ok := ParseRTCPSenderSSRC([]byte{0x80, 0xc8}); ok {
		t.Fatal("short packet must be ok=false")
	}
}

// TestParseRTCPCompoundReal parses the real decrypted SR+report-block+SDES compound captured from
// the official client (the plaintext side of the srtcp KAT).
func TestParseRTCPCompoundReal(t *testing.T) {
	plain, err := hex.DecodeString("81c800127c8657c9edffd5ba8112dc7200024e490000000c000000225dd9e96f000000000000000a00000000d5b873e200001f0000000099000047680000000000000000000000000000000081ca00077c8657c90112343666636240706a6137613564322e6f726700000000")
	if err != nil {
		t.Fatal(err)
	}
	in := ParseRTCPCompound(plain)
	if !in.HasSR {
		t.Fatal("expected HasSR")
	}
	if in.SRNtpMid != 0xd5ba8112 {
		t.Errorf("SRNtpMid = %08x, want d5ba8112", in.SRNtpMid)
	}
	if len(in.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(in.Blocks))
	}
	b := in.Blocks[0]
	if b.SSRC != 0x5dd9e96f || b.LSR != 0xd5b873e2 || b.DLSR != 0x00001f00 || b.FractionLost != 0 {
		t.Errorf("block mismatch: %+v", b)
	}
}

func TestParseRTCPCompoundTruncated(t *testing.T) {
	if in := ParseRTCPCompound([]byte{0x81, 0xc8, 0x00}); in.HasSR || len(in.Blocks) != 0 {
		t.Fatalf("truncated must not parse: %+v", in)
	}
	// SR header claims a length that overruns the buffer: walk must stop, not panic.
	if in := ParseRTCPCompound([]byte{0x81, 0xc8, 0x00, 0xff, 0x01, 0x02, 0x03, 0x04}); in.HasSR {
		t.Fatal("overrunning length must not yield an SR")
	}
}
