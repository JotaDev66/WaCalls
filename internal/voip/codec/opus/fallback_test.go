package opus

import (
	"math"
	"testing"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
)

func newMlow(t *testing.T) core.AudioCodec {
	t.Helper()
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	return codec
}

func sine960() []float32 {
	frame := make([]float32, 960)
	for i := range frame {
		frame[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	return frame
}

func TestFallbackRoutesStandardOpusToOpusDecoder(t *testing.T) {
	codec := WithFallback(newMlow(t))
	defer codec.Close()

	pcm, err := codec.Decode(mustHex(t, celtFrame0))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(pcm) != 320 {
		t.Fatalf("got %d samples, want 320", len(pcm))
	}
	if got := rms(pcm); got < 0.01 {
		t.Fatalf("rms %.5f, want > 0.01: standard-opus frame must decode to audio, not silence", got)
	}
}

func TestFallbackKeepsMlowPathIdentical(t *testing.T) {
	direct := newMlow(t)
	defer direct.Close()
	wrapped := WithFallback(newMlow(t))
	defer wrapped.Close()

	encoded, err := wrapped.Encode(sine960())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded frame is empty")
	}
	if mlow.IsStandardOpusFrame(encoded[0]) {
		t.Fatalf("mlow encoder emitted TOC 0x%02x in the standard-opus range", encoded[0])
	}

	want, err := direct.Decode(encoded)
	if err != nil {
		t.Fatalf("direct Decode: %v", err)
	}
	got, err := wrapped.Decode(encoded)
	if err != nil {
		t.Fatalf("wrapped Decode: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("wrapped decode %d samples, direct %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("sample %d differs: wrapped %v direct %v", i, got[i], want[i])
		}
	}
}

func TestFallbackEmptyPayloadUsesInnerPLC(t *testing.T) {
	codec := WithFallback(newMlow(t))
	defer codec.Close()

	pcm, err := codec.Decode(nil)
	if err != nil {
		t.Fatalf("Decode(nil): %v", err)
	}
	if len(pcm) != 960 {
		t.Fatalf("PLC returned %d samples, want 960", len(pcm))
	}
}

func TestFallbackDelegatesMetadata(t *testing.T) {
	codec := WithFallback(newMlow(t))
	defer codec.Close()

	if codec.FrameSize() != 960 || codec.SampleRate() != 16000 {
		t.Fatalf("unexpected frame=%d rate=%d", codec.FrameSize(), codec.SampleRate())
	}
}
