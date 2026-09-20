package media

import (
	"bytes"
	"testing"
	"wacalls/internal/voip/core"
)

func TestVP8RoundTripSinglePacket(t *testing.T) {
	// A small "keyframe": first payload byte even (P=0).
	frame := append([]byte{0x00}, bytes.Repeat([]byte{0xAB}, 40)...)

	pay := NewVP8Payloader(0x11223344)
	pkts := pay.Packetize(frame, VideoTimestamp(0))
	if len(pkts) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(pkts))
	}
	if pkts[0].Header.PayloadType != core.PayloadTypeWhatsAppVP8 {
		t.Fatalf("payload type = %d", pkts[0].Header.PayloadType)
	}
	if !pkts[0].Header.Marker {
		t.Fatal("single packet must have the marker bit")
	}
	if pkts[0].Payload[0] != vp8DescStart {
		t.Fatalf("descriptor = %#x, want %#x", pkts[0].Payload[0], vp8DescStart)
	}

	dp := NewVP8Depacketizer()
	got, kf, ok := dp.Push(pkts[0])
	if !ok {
		t.Fatal("depacketizer did not complete the frame")
	}
	if !kf {
		t.Fatal("expected keyframe")
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("frame mismatch:\n got %x\nwant %x", got, frame)
	}
}

func TestVP8RoundTripFragmented(t *testing.T) {
	frame := make([]byte, MaxVP8PayloadSize*2+250)
	for i := range frame {
		frame[i] = byte(i)
	}
	frame[0] = 0x01 // odd => interframe (P=1)

	pay := NewVP8Payloader(7)
	pkts := pay.Packetize(frame, VideoTimestamp(33))
	if len(pkts) != 3 {
		t.Fatalf("expected 3 packets, got %d", len(pkts))
	}
	for i, p := range pkts {
		if got, want := p.Header.Marker, i == len(pkts)-1; got != want {
			t.Fatalf("packet %d marker = %v, want %v", i, got, want)
		}
		if got, want := p.Payload[0], byte(vp8DescCont); i > 0 && got != want {
			t.Fatalf("packet %d descriptor = %#x, want %#x", i, got, want)
		}
		if p.Header.Timestamp != pkts[0].Header.Timestamp {
			t.Fatalf("packet %d timestamp differs from first", i)
		}
	}

	dp := NewVP8Depacketizer()
	var out []byte
	var done bool
	var kf bool
	for _, p := range pkts {
		out, kf, done = dp.Push(p)
	}
	if !done {
		t.Fatal("frame never completed")
	}
	if kf {
		t.Fatal("expected interframe, got keyframe")
	}
	if !bytes.Equal(out, frame) {
		t.Fatal("reassembled frame does not match original")
	}
}

func TestVP8DepacketizerDropsOnSequenceGap(t *testing.T) {
	frame := make([]byte, MaxVP8PayloadSize+100)
	pay := NewVP8Payloader(1)
	pkts := pay.Packetize(frame, 0)
	if len(pkts) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(pkts))
	}

	dp := NewVP8Depacketizer()
	if _, _, ok := dp.Push(pkts[0]); ok {
		t.Fatal("first fragment should not complete a frame")
	}
	// Skip a sequence number: bump the second packet's seq so it looks like a
	// packet was lost between them.
	pkts[1].Header.SequenceNumber += 2
	if _, _, ok := dp.Push(pkts[1]); ok {
		t.Fatal("frame with a mid-frame gap must be dropped, not completed")
	}
}

func TestVP8DepacketizerIgnoresContinuationWithoutStart(t *testing.T) {
	dp := NewVP8Depacketizer()
	cont := &RtpPacket{
		Header:  NewRtpHeader(core.PayloadTypeWhatsAppVP8, 5, 0, 1),
		Payload: []byte{vp8DescCont, 0x01, 0x02},
	}
	cont.Header.Marker = true
	if _, _, ok := dp.Push(cont); ok {
		t.Fatal("a continuation packet with no start must not yield a frame")
	}
}

func TestVP8DescriptorLenExtended(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    int
	}{
		{"mandatory only", []byte{0x10, 0xAA}, 1},
		{"X with no optionals", []byte{0x90, 0x00, 0xAA}, 2},
		{"X+I short pictureid", []byte{0x90, 0x80, 0x7F, 0xAA}, 3},
		{"X+I long pictureid", []byte{0x90, 0x80, 0x80, 0x01, 0xAA}, 4},
		{"X+I+L+T", []byte{0x90, 0xF0, 0x7F, 0x11, 0x22, 0xAA}, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := vp8DescriptorLen(c.payload)
			if !ok || got != c.want {
				t.Fatalf("vp8DescriptorLen = (%d, %v), want (%d, true)", got, ok, c.want)
			}
		})
	}
}
