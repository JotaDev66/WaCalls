//go:build nativemlow

package nativemlow

import (
	"math"
	"testing"

	"wacalls/internal/voip/codec/mlow"
)

func tone(frames int) []float32 {
	pcm := make([]float32, frames*frameSamples)
	for i := range pcm {
		tt := float64(i) / float64(sampleRate)
		pcm[i] = float32(0.5 * math.Sin(2.0*math.Pi*550.0*tt))
	}
	return pcm
}

// bestCorr searches the alignment shift (decoder delay differs per implementation).
func bestCorr(ref, x []float32) float64 {
	best := 0.0
	for shift := 0; shift <= 256; shift += 8 {
		n := min(len(ref), len(x)-shift)
		if n < frameSamples {
			break
		}
		var sxy, sxx, syy float64
		for i := range n {
			a, b := float64(ref[i]), float64(x[i+shift])
			sxy += a * b
			sxx += a * a
			syy += b * b
		}
		if sxx > 1e-12 && syy > 1e-12 {
			if c := sxy / math.Sqrt(sxx*syy); c > best {
				best = c
			}
		}
	}
	return best
}

// TestNativeFramesDecodeOnGoDecoder is the permanent wire-compat oracle: the
// native encoder's frames must decode on the byte-exact Go decoder (which is
// validated against real WhatsApp captures) and reconstruct the input.
func TestNativeFramesDecodeOnGoDecoder(t *testing.T) {
	enc, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec := mlow.NewMlowDecoder()

	pcm := tone(8)
	var out []float32
	for f := range 8 {
		frame, err := enc.Encode(pcm[f*frameSamples : (f+1)*frameSamples])
		if err != nil {
			t.Fatalf("frame %d: %v", f, err)
		}
		if len(frame) == 0 {
			t.Fatalf("frame %d: empty", f)
		}
		toc := mlow.ParseSmplTOC(frame[0])
		if toc.StdOpus {
			t.Fatalf("frame %d: TOC 0x%02x parsed as standard opus, not SMPL", f, frame[0])
		}
		out = append(out, dec.Decode(frame)...)
	}
	if c := bestCorr(pcm, out); c < 0.9 {
		t.Fatalf("native frames do not reconstruct the tone on the Go decoder: corr=%.4f", c)
	}
}

// TestSilenceAndTransitions covers the mandatory production input the send loop
// guarantees: literal all-zero PCM at call start and on mic idle, plus the
// transitions. Every tick must yield a decodable frame (no DTX cadence gaps).
func TestSilenceAndTransitions(t *testing.T) {
	enc, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec := mlow.NewMlowDecoder()

	silence := make([]float32, frameSamples)
	tonePcm := tone(4)
	inputs := make([][]float32, 0, 12)
	for range 4 {
		inputs = append(inputs, silence)
	}
	for f := range 4 {
		inputs = append(inputs, tonePcm[f*frameSamples:(f+1)*frameSamples])
	}
	for range 4 {
		inputs = append(inputs, silence)
	}

	for i, in := range inputs {
		frame, err := enc.Encode(in)
		if err != nil {
			t.Fatalf("tick %d: encode: %v", i, err)
		}
		if len(frame) == 0 {
			t.Fatalf("tick %d: empty frame (DTX gap?)", i)
		}
		if pcm := dec.Decode(frame); len(pcm) != frameSamples {
			t.Fatalf("tick %d: Go decoder returned %d samples, want %d", i, len(pcm), frameSamples)
		}
	}
}

// TestEncoderLifecycle: contract errors, double-Close safety, and the live
// counter returning to zero (the leak assertion the composite Close depends on).
func TestEncoderLifecycle(t *testing.T) {
	base := LiveEncoders()
	enc, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	if LiveEncoders() != base+1 {
		t.Fatalf("live=%d want %d", LiveEncoders(), base+1)
	}
	if _, err := enc.Encode(make([]float32, 959)); err == nil {
		t.Fatal("959 samples must error")
	}
	enc.Close()
	enc.Close()
	if LiveEncoders() != base {
		t.Fatalf("live=%d after close, want %d", LiveEncoders(), base)
	}
	if _, err := enc.Encode(make([]float32, frameSamples)); err == nil {
		t.Fatal("encode after close must error")
	}
}

// TestCompositeCloseClosesNative: the composite must destroy the native encoder
// AND close the inner codec (the grill blocker this design revision fixed).
func TestCompositeCloseClosesNative(t *testing.T) {
	base := LiveEncoders()
	inner, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatal(err)
	}
	c, mode := WrapEncoder(inner)
	if mode != "native" {
		t.Fatalf("mode=%q, want native", mode)
	}
	if LiveEncoders() != base+1 {
		t.Fatalf("live=%d after wrap, want %d", LiveEncoders(), base+1)
	}
	if frame, err := c.Encode(make([]float32, frameSamples)); err != nil || len(frame) == 0 {
		t.Fatalf("composite encode: %v (len=%d)", err, len(frame))
	}
	c.Close()
	if LiveEncoders() != base {
		t.Fatalf("live=%d after composite close, want %d", LiveEncoders(), base)
	}
}

// TestConcurrentEncodeClose gives the race detector the real production shape:
// audio.Detach can Close while the send loop's last Encode is in flight.
func TestConcurrentEncodeClose(t *testing.T) {
	for range 50 {
		enc, err := NewEncoder()
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			pcm := make([]float32, frameSamples)
			for range 4 {
				if _, err := enc.Encode(pcm); err != nil {
					return
				}
			}
		}()
		enc.Close()
		<-done
	}
	if n := LiveEncoders(); n != 0 {
		t.Fatalf("live=%d after concurrent close loop", n)
	}
}
