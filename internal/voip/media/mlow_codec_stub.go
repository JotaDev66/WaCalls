//go:build !mlow

package media

import "errors"

var ErrCodecUnavailable = errors.New("MLow codec unavailable: rebuild with -tags mlow and CGO_ENABLED=1")

func NewMLowCodec(opts CodecOptions) (Codec, error) {
	// MLow is pure-Go now — available even without cgo / the mlow build tag.
	return newNativeMLow(), nil
}

func NewOpusCodec(sampleRate, frameSize int) (Codec, error) {
	return nil, ErrCodecUnavailable
}
