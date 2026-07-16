package opus

import (
	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
)

type fallbackCodec struct {
	inner core.AudioCodec
	opus  *Decoder
}

// WithFallback routes inbound standard-Opus frames (first byte & 0xC0 == 0xC0,
// sent by peers without MLow) to a stock Opus decoder and everything else to
// inner. Encode and the stream parameters stay on inner: the fallback is
// receive-only.
func WithFallback(inner core.AudioCodec) core.AudioCodec {
	dec, err := NewDecoder()
	if err != nil {
		return inner
	}
	return &fallbackCodec{inner: inner, opus: dec}
}

func (c *fallbackCodec) Decode(frame []byte) ([]float32, error) {
	if len(frame) > 0 && mlow.IsStandardOpusFrame(frame[0]) {
		return c.opus.Decode(frame), nil
	}
	return c.inner.Decode(frame)
}

func (c *fallbackCodec) Encode(pcm []float32) ([]byte, error) { return c.inner.Encode(pcm) }

func (c *fallbackCodec) FrameSize() int { return c.inner.FrameSize() }

func (c *fallbackCodec) SampleRate() int { return c.inner.SampleRate() }

func (c *fallbackCodec) Close() { c.inner.Close() }
