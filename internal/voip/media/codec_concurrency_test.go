//go:build mlow

package media

import (
	"math"
	"sync"
	"testing"
)

// These tests guard the thread-safety of the bundled opus_mlow library.
// In a live call, Encode (uplink) and Decode (downlink) run on different
// goroutines against the same codec object. The native library is not
// thread-safe, so the codec implementation must serialize every C call;
// without that, concurrent Encode/Decode crashes the process
// ("fatal error: semasleep on Darwin signal stack").

func testTone(n int, freq, rate float64) []float32 {
	f := make([]float32, n)
	for i := range f {
		f[i] = 0.3 * float32(math.Sin(2*math.Pi*freq*float64(i)/rate))
	}
	return f
}

func hammerEncodeDecode(t *testing.T, c Codec, frame []float32) {
	t.Helper()
	seed, err := c.Encode(frame)
	if err != nil {
		t.Fatalf("seed encode: %v", err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				_, _ = c.Encode(frame)
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				_, _ = c.Decode(seed)
			}
		}()
	}
	wg.Wait()
}

// Opus 48 kHz codec (browser side): Encode runs on the relay goroutine
// (OnPeerAudio) while Decode runs on the bridge goroutine (OnBrowserRTP).
func TestOpus48ConcurrentEncodeDecode(t *testing.T) {
	c, err := NewOpusCodec(48000, 960)
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	defer c.Close()
	hammerEncodeDecode(t, c, testTone(2880, 440, 48000)) // 60 ms @ 48k
}

// MLow codec (WhatsApp side): Encode runs under the CallManager lock
// (FeedCapturedPCM / keepalive) while Decode runs from onRelayData.
func TestMLowConcurrentEncodeDecode(t *testing.T) {
	c, err := NewMLowCodec(DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer c.Close()
	hammerEncodeDecode(t, c, testTone(960, 440, 16000))
}

// The Opus 48 kHz roundtrip is otherwise uncovered; it exercises the exact
// frame size produced by Upsample16to48 of a 960-sample 16 kHz frame.
func TestOpus48Roundtrip(t *testing.T) {
	c, err := NewOpusCodec(48000, 960)
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	defer c.Close()
	frame := testTone(2880, 440, 48000)
	for i := 0; i < 500; i++ {
		enc, err := c.Encode(frame)
		if err != nil {
			t.Fatalf("Encode iter %d: %v", i, err)
		}
		if _, err := c.Decode(enc); err != nil {
			t.Fatalf("Decode iter %d: %v", i, err)
		}
	}
}
