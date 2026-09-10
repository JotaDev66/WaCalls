package media

import (
	"encoding/binary"
	"testing"
)

// buildNACK monta um RTPFB/NACK cru (RFC 4585) com um FCI por (pid, blp).
func buildNACK(senderSSRC, mediaSSRC uint32, fci [][2]uint16) []byte {
	b := make([]byte, 12+4*len(fci))
	b[0] = 0x81 // V=2, FMT=1 (NACK genérico)
	b[1] = RTCPTypeRTPFB
	binary.BigEndian.PutUint16(b[2:], uint16(len(b)/4-1))
	binary.BigEndian.PutUint32(b[4:], senderSSRC)
	binary.BigEndian.PutUint32(b[8:], mediaSSRC)
	for i, f := range fci {
		binary.BigEndian.PutUint16(b[12+i*4:], f[0])
		binary.BigEndian.PutUint16(b[12+i*4+2:], f[1])
	}
	return b
}

func TestParseNACK(t *testing.T) {
	// pid=100 com blp cobrindo 101 (bit0) e 105 (bit4); segundo FCI pid=200 sem blp.
	pkt := buildNACK(0xAAAA, 0xBBBB, [][2]uint16{{100, 0b1_0001}, {200, 0}})

	media, seqs, ok := ParseNACK(pkt)
	if !ok {
		t.Fatal("ParseNACK devia reconhecer o pacote")
	}
	if media != 0xBBBB {
		t.Errorf("media ssrc = %#x, quer 0xbbbb", media)
	}
	want := map[uint16]bool{100: true, 101: true, 105: true, 200: true}
	if len(seqs) != len(want) {
		t.Fatalf("seqs = %v, quer %d itens", seqs, len(want))
	}
	for _, s := range seqs {
		if !want[s] {
			t.Errorf("seq inesperado %d em %v", s, seqs)
		}
	}
}

func TestParseNACKRejectsOthers(t *testing.T) {
	sr := BuildSenderReport(1, 2, 3, 4)
	if _, _, ok := ParseNACK(sr); ok {
		t.Error("um SR não é NACK")
	}
	remb := BuildREMB(1, 2, 500000)
	if _, _, ok := ParseNACK(remb); ok {
		t.Error("um REMB não é NACK")
	}
}

func TestIsPLIOrFIR(t *testing.T) {
	pli := make([]byte, 12)
	pli[0] = 0x81 // FMT=1
	pli[1] = RTCPTypePSFB
	if !IsPLIOrFIR(pli) {
		t.Error("PSFB/PLI devia ser reconhecido")
	}
	fir := make([]byte, 12)
	fir[0] = 0x84 // FMT=4
	fir[1] = RTCPTypePSFB
	if !IsPLIOrFIR(fir) {
		t.Error("PSFB/FIR devia ser reconhecido")
	}
	remb := BuildREMB(1, 2, 500000) // PSFB FMT=15
	if IsPLIOrFIR(remb) {
		t.Error("REMB (FMT 15) não é PLI/FIR")
	}
}
