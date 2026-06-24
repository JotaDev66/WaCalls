package media

import "wacalls/internal/voip/media/mlow"

// mlowNativeCodec is the pure-Go MLow codec (no cgo), backed by the
// github.com/purpshell/meowcaller/mlow port of Meta's SMPL/MLow codec. It
// replaces the former cgo opus_mlow implementation for the WhatsApp-side audio,
// so MLow no longer needs the compiled libopus_mlow.a. Encode and Decode use
// separate encoder/decoder instances and are safe to call from different
// goroutines (uplink encodes while downlink decodes).
type mlowNativeCodec struct {
	enc *mlow.MlowEncoder
	dec *mlow.MlowDecoder
}

func newNativeMLow() Codec {
	return &mlowNativeCodec{
		enc: mlow.NewMlowEncoder(),
		dec: mlow.NewMlowDecoder(),
	}
}

func (c *mlowNativeCodec) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, nil
	}
	return c.enc.Encode(pcm)
}

func (c *mlowNativeCodec) Decode(frame []byte) ([]float32, error) {
	// The caller normalizes the frame length via media.NormalizeFrame, so a
	// short or empty result here is fine.
	return c.dec.Decode(frame), nil
}

func (c *mlowNativeCodec) FrameSize() int  { return mlowFrameSize }
func (c *mlowNativeCodec) SampleRate() int { return mlowSampleRate }
func (c *mlowNativeCodec) Close()          {}
