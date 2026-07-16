package mlow

import (
	"math"
	"testing"
)

func benchToneFrames(frames int) [][]float32 {
	out := make([][]float32, frames)
	for f := range frames {
		pcm := make([]float32, mlowFrameSize)
		for i := range pcm {
			tt := float64(f*mlowFrameSize+i) / float64(mlowSampleRate)
			pcm[i] = float32(0.5 * math.Sin(2.0*math.Pi*550.0*tt))
		}
		out[f] = pcm
	}
	return out
}

func benchSignal(n int) []float32 {
	x := make([]float32, n)
	var s uint32 = 12345
	for i := range x {
		s = s*196314165 + 907633515
		x[i] = float32(s>>9)/float32(uint32(1)<<23) - 1.0
	}
	return x
}

func BenchmarkEncode(b *testing.B) {
	frames := benchToneFrames(8)
	enc := NewMlowEncoder()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := enc.Encode(frames[i%len(frames)]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	frames := benchToneFrames(8)
	enc := NewMlowEncoder()
	encoded := make([][]byte, len(frames))
	for i, pcm := range frames {
		fr, err := enc.Encode(pcm)
		if err != nil {
			b.Fatal(err)
		}
		encoded[i] = fr
	}
	dec := NewMlowDecoder()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		dec.Decode(encoded[i%len(encoded)])
	}
}

func BenchmarkRfftForward512(b *testing.B) { benchRfftForward(b, 512) }
func BenchmarkRfftForward576(b *testing.B) { benchRfftForward(b, 576) }

func benchRfftForward(b *testing.B, n int) {
	x := benchSignal(n)
	f := make([]float32, n)
	b.ReportAllocs()
	for b.Loop() {
		rfftForwardOrdered(x, f)
	}
}

func BenchmarkRfftBackward576(b *testing.B) {
	const n = 576
	x := benchSignal(n)
	f := make([]float32, n)
	rfftForwardOrdered(x, f)
	tout := make([]float32, n)
	b.ReportAllocs()
	for b.Loop() {
		rfftBackwardOrdered(f, tout)
	}
}
