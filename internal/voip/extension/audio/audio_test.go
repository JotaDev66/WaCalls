package audio

import (
	"math"
	"testing"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

func TestAudioDecodesInbound(t *testing.T) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	a := New(codec)
	var got []float32
	a.OnPeerPCM(func(p []float32) { got = p })

	frame := make([]float32, codec.FrameSize())
	for i := range frame {
		frame[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	enc, err := codec.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	pkt := &media.RtpPacket{
		Header:  media.NewRtpHeader(core.PayloadTypeWhatsAppOpus, 1, 0, 5000),
		Payload: enc,
	}
	a.handleInbound(pkt)

	if len(got) == 0 {
		t.Fatal("expected decoded peer PCM, got none")
	}
}

func TestAudioConcealsLostPacket(t *testing.T) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	a := New(codec)
	var got [][]float32
	a.OnPeerPCM(func(p []float32) { got = append(got, p) })

	frame := make([]float32, codec.FrameSize())
	for i := range frame {
		frame[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	enc, err := codec.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	push := func(seq uint16) {
		a.handleInbound(&media.RtpPacket{
			Header:  media.NewRtpHeader(core.PayloadTypeWhatsAppOpus, seq, 0, 5000),
			Payload: enc,
		})
	}

	push(1) // decoded, becomes lastFrame
	push(3) // seq 2 missing: hold
	push(4) // hold
	push(5) // depth reached: conceal 2, then flush 3,4,5

	// Frames: 1 (real), then concealed-2, 3, 4, 5 = 5 emitted PCM frames.
	if len(got) != 5 {
		t.Fatalf("want 5 played frames (1 real, 1 concealed, 3 real), got %d", len(got))
	}
	// The concealment is a faded copy of the last decoded frame: it starts near the
	// last frame's first sample and fades to zero.
	concealed := got[1]
	if len(concealed) == 0 || concealed[len(concealed)-1] != 0 {
		t.Fatalf("concealment must fade to zero at the tail, got tail %v", concealed[len(concealed)-1])
	}
}

func TestAudioFeedPCMBounds(t *testing.T) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	a := New(codec)
	a.FeedPCM(make([]float32, codec.FrameSize()*10))

	if len(a.captureBuf) > codec.FrameSize()*4 {
		t.Fatalf("captureBuf=%d exceeds bound %d", len(a.captureBuf), codec.FrameSize()*4)
	}
}
