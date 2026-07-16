//go:build nativemlow

package nativemlow

import "wacalls/internal/voip/core"

// codec routes Encode to the native SMPL encoder and everything else to the
// wrapped pure-Go codec. Close MUST release both: the native C state is a real
// resource freed exactly once per call via audio.Detach -> codec.Close.
type codec struct {
	inner core.AudioCodec
	enc   *Encoder
}

// WrapEncoder swaps the encode path of inner for the native encoder. If the
// native encoder cannot be created it returns inner unchanged (callers keep the
// pure-Go behavior).
func WrapEncoder(inner core.AudioCodec) core.AudioCodec {
	enc, err := NewEncoder()
	if err != nil {
		return inner
	}
	return &codec{inner: inner, enc: enc}
}

func (c *codec) Encode(pcm []float32) ([]byte, error) {
	out, err := c.enc.Encode(pcm)
	if err != nil {
		return c.inner.Encode(pcm)
	}
	return out, nil
}

func (c *codec) Decode(frame []byte) ([]float32, error) { return c.inner.Decode(frame) }
func (c *codec) FrameSize() int                         { return c.inner.FrameSize() }
func (c *codec) SampleRate() int                        { return c.inner.SampleRate() }

func (c *codec) Close() {
	c.enc.Close()
	c.inner.Close()
}

// Available reports whether the native encoder is compiled in.
func Available() bool { return true }

// Mode names the active encode path for operator logs.
func Mode() string { return "native" }
