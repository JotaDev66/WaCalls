package media

import (
	"math"
	"sync"
	"testing"

	"wacalls/internal/voip/media/mlow"
)

// These tests prove the pure-Go MLow codec (no cgo) encodes/decodes a
// 960-sample (60 ms @ 16 kHz) frame — the same format our cgo MLow uses.
// Run with CGO_ENABLED=0 to confirm there is no C dependency.

func nativeTone(n int) []float32 {
	f := make([]float32, n)
	for i := range f {
		f[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	return f
}

func TestPureGoMLowRoundtrip(t *testing.T) {
	enc := mlow.NewMlowEncoder()
	dec := mlow.NewMlowDecoder()
	frame := nativeTone(960)
	for i := 0; i < 100; i++ {
		pkt, err := enc.Encode(frame)
		if err != nil {
			t.Fatalf("encode iter %d: %v", i, err)
		}
		if len(pkt) == 0 {
			t.Fatalf("encode iter %d produced an empty packet", i)
		}
		pcm := dec.Decode(pkt)
		if len(pcm) == 0 {
			t.Fatalf("decode iter %d produced empty PCM", i)
		}
	}
}

// Encode (uplink) and Decode (downlink) run on different goroutines in a live
// call. The pure-Go encoder/decoder are separate instances, so this must be
// race-free (run with -race).
func TestPureGoMLowConcurrent(t *testing.T) {
	enc := mlow.NewMlowEncoder()
	dec := mlow.NewMlowDecoder()
	frame := nativeTone(960)
	seed, err := enc.Encode(frame)
	if err != nil {
		t.Fatalf("seed encode: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 3000; i++ {
			_, _ = enc.Encode(frame)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 3000; i++ {
			_ = dec.Decode(seed)
		}
	}()
	wg.Wait()
}
