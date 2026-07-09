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
