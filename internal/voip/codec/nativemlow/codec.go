//go:build nativemlow

package nativemlow

import "wacalls/internal/voip/core"

// codec routes Encode to the native SMPL encoder and everything else to the
// wrapped pure-Go codec. Close MUST release both: the native C state is a real
// resource freed exactly once per call via audio.Detach -> codec.Close.
//
// There is deliberately NO per-frame fallback to the inner encoder: the Go
// encoder carries cross-frame state, and feeding it only the frames the native
// path failed on would splice a differently-stateful encoder's output into a
// continuous bitstream. A failed frame surfaces as an error (the send loop
// drops that tick); only construction-time fallback is state-safe.
type codec struct {
	inner core.AudioCodec
	enc   *Encoder
}

// WrapEncoder swaps the encode path of inner for the native encoder and reports
// the encode path actually live: "native", or "go" when the native encoder
// could not be created and inner is returned unchanged.
func WrapEncoder(inner core.AudioCodec) (core.AudioCodec, string) {
	enc, err := NewEncoder()
	if err != nil {
		return inner, "go"
	}
	return &codec{inner: inner, enc: enc}, "native"
}

func (c *codec) Encode(pcm []float32) ([]byte, error) { return c.enc.Encode(pcm) }

func (c *codec) Decode(frame []byte) ([]float32, error) { return c.inner.Decode(frame) }
func (c *codec) FrameSize() int                         { return c.inner.FrameSize() }
func (c *codec) SampleRate() int                        { return c.inner.SampleRate() }

func (c *codec) Close() {
	c.enc.Close()
	c.inner.Close()
}

// Available reports whether the native encoder is compiled in.
func Available() bool { return true }
