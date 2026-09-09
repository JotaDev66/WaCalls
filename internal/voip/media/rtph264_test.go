package media

import (
	"bytes"
	"testing"
)

func TestSplitAnnexB(t *testing.T) {
	sc4 := []byte{0, 0, 0, 1}
	sc3 := []byte{0, 0, 1}
	frame := bytes.Join([][]byte{
		sc4, {0x67, 0x42, 0x00},
		sc3, {0x68, 0xce},
		sc4, {0x65, 0x11, 0x22, 0x33},
	}, nil)

	nalus := splitAnnexB(frame)
	if len(nalus) != 3 {
		t.Fatalf("want 3 NALUs, got %d", len(nalus))
	}
	if nalus[0][0] != 0x67 || nalus[1][0] != 0x68 || nalus[2][0] != 0x65 {
		t.Fatalf("NALU headers wrong: %x %x %x", nalus[0][0], nalus[1][0], nalus[2][0])
	}
	if !bytes.Equal(nalus[2], []byte{0x65, 0x11, 0x22, 0x33}) {
		t.Fatalf("last NALU wrong: %x", nalus[2])
	}
}

func TestH264PayloadRoundTrip(t *testing.T) {
	sc := []byte{0, 0, 0, 1}
	sps := append([]byte{0x67}, bytes.Repeat([]byte{0x42}, 8)...)
	idr := append([]byte{0x65}, bytes.Repeat([]byte{0x11}, MaxH264PayloadSize*2+5)...) // força FU-A
	frame := bytes.Join([][]byte{sc, sps, sc, idr}, nil)

	pay := NewH264Payloader(0xABCD)
	pkts := pay.Packetize(frame, 90000)
	if len(pkts) < 3 {
		t.Fatalf("esperava >=3 pacotes (SPS + FU-A), got %d", len(pkts))
	}
	if !pkts[len(pkts)-1].Header.Marker {
		t.Fatal("marker bit não está no último pacote")
	}

	depay := NewH264Depacketizer()
	var out []byte
	var gotKeyframe bool
	for _, p := range pkts {
		f, kf, ok := depay.Push(p)
		if ok {
			out = f
			gotKeyframe = kf
		}
	}
	if !gotKeyframe {
		t.Fatal("keyframe não detectado")
	}
	if !bytes.Equal(out, frame) {
		t.Fatalf("frame mudou no round trip: %d bytes vs %d", len(out), len(frame))
	}
}

func TestCVORotationDegrees(t *testing.T) {
	// Bloco de um byte (profile 0xDEBE) com CVO id 3 = 0x03 (rotação 270).
	h := &RtpHeader{Extension: true, ExtensionProfile: 0xDEBE, ExtensionData: []byte{0x30, 0x03, 0x00, 0x00}}
	deg, ok := CVORotationDegrees(h)
	if !ok || deg != 270 {
		t.Fatalf("want (270, true), got (%d, %v)", deg, ok)
	}

	// Sem extensão → ok=false.
	if _, ok := CVORotationDegrees(&RtpHeader{}); ok {
		t.Fatal("esperava ok=false sem extensão")
	}
}
