package media

import (
	"bytes"
	"testing"
)

func TestVideoFrameRoundTrip(t *testing.T) {
	in := VideoFrame{Keyframe: true, TimestampMS: 123456, Data: []byte{1, 2, 3, 4, 5}}
	out, ok := DecodeVideoFrame(EncodeVideoFrame(in))
	if !ok {
		t.Fatal("decode failed")
	}
	if out.Keyframe != in.Keyframe || out.TimestampMS != in.TimestampMS {
		t.Fatalf("header mismatch: %+v vs %+v", out, in)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Fatalf("data mismatch: %x vs %x", out.Data, in.Data)
	}
}

func TestVideoFrameInterframeFlag(t *testing.T) {
	out, ok := DecodeVideoFrame(EncodeVideoFrame(VideoFrame{Keyframe: false, TimestampMS: 0, Data: nil}))
	if !ok {
		t.Fatal("decode failed")
	}
	if out.Keyframe {
		t.Fatal("keyframe flag should be false")
	}
	if len(out.Data) != 0 {
		t.Fatalf("expected empty data, got %x", out.Data)
	}
}

func TestDecodeVideoFrameTooShort(t *testing.T) {
	if _, ok := DecodeVideoFrame([]byte{0x00, 0x01}); ok {
		t.Fatal("a message shorter than the header must not decode")
	}
}
