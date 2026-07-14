package media

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// SDES layout is pinned byte-for-byte against a real WhatsApp SDES (same ssrc + cname).
func TestBuildSDESMatchesClient(t *testing.T) {
	got := BuildSDES(0x7c8657c9, "46fcb@pja7a5d2.org")
	want := "81ca00077c8657c90112343666636240706a6137613564322e6f726700000000"
	if hex.EncodeToString(got) != want {
		t.Errorf("SDES mismatch\n got  %s\n want %s", hex.EncodeToString(got), want)
	}
}

// Report block layout is pinned against the block in a real WhatsApp Sender Report.
func TestSenderReportBlockLayout(t *testing.T) {
	rb := &RTCPReportBlock{
		SSRC: 0x5dd9e96f, FractionLost: 0, CumulativeLost: 0,
		ExtHighSeq: 10, Jitter: 0, LSR: 0xd5b873e2, DLSR: 0x00001f00,
	}
	sr := BuildSenderReportWithBlock(0x7c8657c9, RTCPSenderStats{}, rb, 0)
	if len(sr) != 52 {
		t.Fatalf("SR len = %d, want 52", len(sr))
	}
	if sr[0] != 0x81 { // V=2, RC=1
		t.Errorf("SR byte0 = %#x, want 0x81", sr[0])
	}
	if got := binary.BigEndian.Uint16(sr[2:4]); got != 12 {
		t.Errorf("SR length field = %d, want 12", got)
	}
	if got, want := hex.EncodeToString(sr[28:52]), "5dd9e96f000000000000000a00000000d5b873e200001f00"; got != want {
		t.Errorf("report block layout mismatch\n got  %s\n want %s", got, want)
	}
}

func TestSenderReportNoBlock(t *testing.T) {
	sr := BuildSenderReportWithBlock(0x11223344, RTCPSenderStats{RtpTimestamp: 0x1000, PacketsSent: 5, OctetsSent: 600}, nil, 1000)
	if len(sr) != 28 || sr[0] != 0x80 || binary.BigEndian.Uint16(sr[2:4]) != 6 {
		t.Fatalf("SR-no-block malformed: len=%d b0=%#x lenfield=%d", len(sr), sr[0], binary.BigEndian.Uint16(sr[2:4]))
	}
	if binary.BigEndian.Uint32(sr[20:24]) != 5 || binary.BigEndian.Uint32(sr[24:28]) != 600 {
		t.Error("SR sender counts misplaced")
	}
}

func TestRTCPCompoundStructure(t *testing.T) {
	rb := &RTCPReportBlock{SSRC: 0x5dd9e96f, ExtHighSeq: 10, LSR: 0xd5b873e2, DLSR: 0x1f00}
	c := BuildRTCPCompound(0x7c8657c9, RTCPSenderStats{}, rb, "test@wacalls", 0)
	if c[1] != RTCPPayloadTypeSR {
		t.Fatalf("first PT = %d, want SR(%d)", c[1], RTCPPayloadTypeSR)
	}
	srLen := (int(binary.BigEndian.Uint16(c[2:4])) + 1) * 4
	if srLen != 52 {
		t.Fatalf("SR len = %d, want 52", srLen)
	}
	sdes := c[srLen:]
	if sdes[1] != RTCPPayloadTypeSDES {
		t.Fatalf("second PT = %d, want SDES(%d)", sdes[1], RTCPPayloadTypeSDES)
	}
	sdesLen := (int(binary.BigEndian.Uint16(sdes[2:4])) + 1) * 4
	if srLen+sdesLen != len(c) {
		t.Errorf("compound length mismatch: sr=%d sdes=%d total=%d", srLen, sdesLen, len(c))
	}
}
