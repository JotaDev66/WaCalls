package mlow

import "wacalls/internal/voip/core"

type mlowCodec struct {
	enc *MlowEncoder
	dec *MlowDecoder
}

func NewMLowCodec(opts CodecOptions) (core.AudioCodec, error) {
	_ = opts
	return &mlowCodec{
		enc: NewMlowEncoder(),
		dec: NewMlowDecoder(),
	}, nil
}

func (c *mlowCodec) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, nil
	}
	return c.enc.Encode(pcm)
}

func (c *mlowCodec) Decode(frame []byte) ([]float32, error) {
	return c.dec.Decode(frame), nil
}

func (c *mlowCodec) FrameSize() int  { return mlowFrameSize }
func (c *mlowCodec) SampleRate() int { return mlowSampleRate }
func (c *mlowCodec) Close()          {}
