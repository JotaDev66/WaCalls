package mlow

type CodecOptions struct {
	Bitrate    int
	Complexity int
	FEC        bool
}

var DefaultCodecOptions = CodecOptions{Bitrate: 6000, Complexity: 5, FEC: false}

const (
	mlowSampleRate = 16000
	mlowFrameSize  = 960
)
