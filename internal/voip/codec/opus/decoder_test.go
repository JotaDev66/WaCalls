package opus

import (
	"encoding/hex"
	"math"
	"testing"
)

// Three consecutive 20 ms CELT-FB mono frames (TOC 0xF8) of a 440 Hz tone,
// encoded once with ffmpeg/libopus (48 kHz mono, -application lowdelay,
// 64 kbps CBR) and embedded so CI needs no encoder.
const (
	celtFrame0 = "f8b04e7f9fc6d1ed077136f42f3ac682c4f17fde50d7458761fb93aa48ecde8240596372c8ae1d51b1dd0782c428b28459b8fdf58db57179cc2a4a2140ed68513d0b06d9a06c01066356124a46776549897d0d4e9d0b087c292b726822654ddb30e2ce12decc90499e11d89b3cc38bcc8737e75b97591166d6427026673e4744864e0c829faf2cc88a7598590afb45da51b9f3c1187ed6317dd5db1ba38d4fee"
	celtFrame1 = "f8b04dcf490c8b79c33c705417116723e7e43e730ee5b2d4d3897ec3bf44acf14d5b5c7aae69a9d2d55cc87b689d885fbde0279f17e39430a6409c661bcebb4c3a52e753725d6ddd89a0dc1dca4b14e0074aa02ac04f4b2461e2cfa3714148805ae4fccabc79f2d121b0e3135546c79c15aacf854b4366d5c1478d582243a9260fd37c5b017f297a7d70d161c2929eb9cc0dbe98fe3ed25fb679aa9ca07afdee"
	celtFrame2 = "f8afe94ff1bbfb04673e0f7cc221b8fc7f5f9b0ac656d77032dec242bdc16fcc6a2f4fcc922b48dcde9ba9115b0ceade31c53b396258fd8133035575d9c77503983eee72f7c5a69f14e88faaf7c0e2d56f1866dcb616e1abf6693ad062c03c7cc73b7c7ca7b63f9441958b4b96a0e59d0c55767c9142bd8b5c9865b6272bac2c93d361e8ff20ae5271d08d897c227a9f6de23e32ee5ed3e0f61bd9777f34bbee"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad vector: %v", err)
	}
	return b
}

func rms(pcm []float32) float64 {
	var sum float64
	for _, s := range pcm {
		sum += float64(s) * float64(s)
	}
	if len(pcm) == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func TestDecodeRealCeltFramesTo16kMono(t *testing.T) {
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	for i, v := range []string{celtFrame0, celtFrame1, celtFrame2} {
		frame := mustHex(t, v)
		if frame[0]&0xC0 != 0xC0 {
			t.Fatalf("vector %d first byte 0x%02x is not a standard-opus TOC", i, frame[0])
		}
		pcm := dec.Decode(frame)
		if len(pcm) != 320 {
			t.Fatalf("frame %d: got %d samples, want 320 (20 ms @ 16 kHz)", i, len(pcm))
		}
		if got := rms(pcm); got < 0.01 {
			t.Fatalf("frame %d: rms %.5f, want > 0.01 (audible tone, not silence)", i, got)
		}
	}
}

func TestDecodeGarbageEmitsTocSizedSilence(t *testing.T) {
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	garbage := append([]byte{0xFF}, []byte("not an opus frame at all")...)
	pcm := dec.Decode(garbage)
	if len(pcm) != 320 {
		t.Fatalf("got %d samples, want 320 (TOC 0xFF => 20 ms @ 16 kHz)", len(pcm))
	}
	for i, s := range pcm {
		if s != 0 {
			t.Fatalf("sample %d = %v, want pure silence on decode error", i, s)
		}
	}
}
