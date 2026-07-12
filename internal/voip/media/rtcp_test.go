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
