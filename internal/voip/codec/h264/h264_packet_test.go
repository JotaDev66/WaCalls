package h264

import (
	"bytes"
	"testing"
)

func depacketizeAll(d *H264Depacketizer, pkts [][]byte) [][]byte {
	var out [][]byte
	for _, p := range pkts {
		out = append(out, d.Depacketize(p)...)
	}
	return out
}

func TestH264SingleNALURoundtrip(t *testing.T) {
	nalu := []byte{0x41, 0x9a, 0x00, 0x11, 0x22}
	pkts := PackageH264NALU(nalu)
	if len(pkts) != 1 {
		t.Fatalf("small NALU should be one packet, got %d", len(pkts))
	}
	var d H264Depacketizer
	got := depacketizeAll(&d, pkts)
	if len(got) != 1 || !bytes.Equal(got[0], nalu) {
		t.Fatalf("roundtrip mismatch: got %x", got)
	}
}

func TestH264FUARoundtrip(t *testing.T) {
	nalu := make([]byte, 5000)
	nalu[0] = 0x65
	for i := 1; i < len(nalu); i++ {
		nalu[i] = byte(i)
	}
	pkts := PackageH264NALU(nalu)
	if len(pkts) < 2 {
		t.Fatalf("large NALU should fragment, got %d packets", len(pkts))
	}
	for _, p := range pkts {
		if p[0]&0x1F != h264FuaType {
			t.Fatalf("fragment is not FU-A: %x", p[0])
		}
	}
	var d H264Depacketizer
	got := depacketizeAll(&d, pkts)
	if len(got) != 1 || !bytes.Equal(got[0], nalu) {
		t.Fatalf("FU-A reassembly mismatch: got %d NALUs", len(got))
	}
}

func TestH264STAPARoundtrip(t *testing.T) {
	sps := []byte{0x67, 0x42, 0x00, 0x1f}
	pps := []byte{0x68, 0xce, 0x3c, 0x80}
	stap := PackageH264STAPA([][]byte{sps, pps})
	if stap == nil || stap[0]&0x1F != h264StapAType {
		t.Fatalf("expected STAP-A aggregate")
	}
	var d H264Depacketizer
	got := d.Depacketize(stap)
	if len(got) != 2 || !bytes.Equal(got[0], sps) || !bytes.Equal(got[1], pps) {
		t.Fatalf("STAP-A split mismatch: got %d NALUs", len(got))
	}
}

func TestSplitAnnexB(t *testing.T) {
	n1 := []byte{0x67, 0x01, 0x02}
	n2 := []byte{0x68, 0x03}
	n3 := []byte{0x65, 0x04, 0x05, 0x06}
	var stream []byte
	stream = append(stream, 0, 0, 0, 1)
	stream = append(stream, n1...)
	stream = append(stream, 0, 0, 1)
	stream = append(stream, n2...)
	stream = append(stream, 0, 0, 0, 1)
	stream = append(stream, n3...)

	nalus := SplitAnnexB(stream)
	if len(nalus) != 3 || !bytes.Equal(nalus[0], n1) || !bytes.Equal(nalus[1], n2) || !bytes.Equal(nalus[2], n3) {
		t.Fatalf("AnnexB split mismatch: got %d NALUs %x", len(nalus), nalus)
	}
}
