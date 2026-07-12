package opus

import (
	pion "github.com/pion/opus"

	"wacalls/internal/voip/codec/mlow"
)

// 120 ms @ 16 kHz, the longest standard Opus packet (RFC 6716).
const maxDecodedSamples = 1920

type Decoder struct {
	dec pion.Decoder
	buf []float32
}

func NewDecoder() (*Decoder, error) {
	dec, err := pion.NewDecoderWithOutput(16000, 1)
	if err != nil {
		return nil, err
	}
	return &Decoder{dec: dec, buf: make([]float32, maxDecodedSamples)}, nil
}

// Decode expects a non-empty frame whose first byte already matched
// mlow.IsStandardOpusFrame. A failed decode yields TOC-sized silence so the
// playout clock keeps the same behavior as the previous silence path.
func (d *Decoder) Decode(frame []byte) []float32 {
	n, err := d.dec.DecodeToFloat32(frame, d.buf)
	if err != nil || n <= 0 {
		return make([]float32, 16*mlow.ParseSmplTOC(frame[0]).FrameMs)
	}
	out := make([]float32, n)
	copy(out, d.buf[:n])
	return out
}
