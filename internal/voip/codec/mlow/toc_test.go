package mlow

import (
	"encoding/json"
	"os"
	"testing"
)

type tocVector struct {
	B      byte `json:"b"`
	Std    bool `json:"std"`
	SID    bool `json:"sid"`
	VAD    bool `json:"vad"`
	SR     int  `json:"sr"`
	Ms     int  `json:"ms"`
	Voiced bool `json:"voiced"`
	Active bool `json:"active"`
	F2     bool `json:"f2"`
	F0     bool `json:"f0"`
}

// TestParseSmplTOC checks ParseSmplTOC against the reference vectors, exhaustive
// over all 256 byte values.
func TestParseSmplTOC(t *testing.T) {
	raw, err := os.ReadFile("testdata/toc_vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vecs []tocVector
	if err := json.Unmarshal(raw, &vecs); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}
	if len(vecs) != 256 {
		t.Fatalf("expected 256 vectors, got %d", len(vecs))
	}

	for _, v := range vecs {
		got := ParseSmplTOC(v.B)
		if got.StdOpus != v.Std {
			t.Errorf("b=0x%02x StdOpus: got %v want %v", v.B, got.StdOpus, v.Std)
		}
		if got.SID != v.SID {
			t.Errorf("b=0x%02x SID: got %v want %v", v.B, got.SID, v.SID)
		}
		if got.VAD != v.VAD {
			t.Errorf("b=0x%02x VAD: got %v want %v", v.B, got.VAD, v.VAD)
		}
		if got.SampleRate != v.SR {
			t.Errorf("b=0x%02x SampleRate: got %v want %v", v.B, got.SampleRate, v.SR)
		}
		if got.FrameMs != v.Ms {
			t.Errorf("b=0x%02x FrameMs: got %v want %v", v.B, got.FrameMs, v.Ms)
		}
		if got.Voiced != v.Voiced {
			t.Errorf("b=0x%02x Voiced: got %v want %v", v.B, got.Voiced, v.Voiced)
		}
		if got.Active != v.Active {
			t.Errorf("b=0x%02x Active: got %v want %v", v.B, got.Active, v.Active)
		}
		if got.Flag2 != v.F2 {
			t.Errorf("b=0x%02x Flag2: got %v want %v", v.B, got.Flag2, v.F2)
		}
		if got.Flag0 != v.F0 {
			t.Errorf("b=0x%02x Flag0: got %v want %v", v.B, got.Flag0, v.F0)
		}
	}
}

// TestIsStandardOpusFrameBoundary pins the inbound router split exactly as the
// reference does: every 0xC0..=0xFF byte is stock Opus, and real MLow TOCs
// (bare frames and the 0x92 SplitRed container) are not.
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/main/wacore/src/voip/mlow/mod.rs (opus_split_boundary_table)
func TestIsStandardOpusFrameBoundary(t *testing.T) {
	for b := 0xC0; b <= 0xFF; b++ {
		if !IsStandardOpusFrame(byte(b)) {
			t.Errorf("0x%02x must be standard opus", b)
		}
	}
	for _, mlowToc := range []byte{0x10, 0x12, 0x50, 0x92} {
		if IsStandardOpusFrame(mlowToc) {
			t.Errorf("mlow TOC 0x%02x must not route to standard opus", mlowToc)
		}
	}
	for b := 0; b <= 0xFF; b++ {
		if IsStandardOpusFrame(byte(b)) != ParseSmplTOC(byte(b)).StdOpus {
			t.Errorf("0x%02x: IsStandardOpusFrame disagrees with ParseSmplTOC.StdOpus", b)
		}
	}
}
