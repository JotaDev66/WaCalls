//go:build nativemlow

// Package nativemlow binds the SMPL/MLow encoder from edgardmessias/opus_mlow
// (libopus 1.4 fork, pinned at 93e91a74c0a2af610d8313a85e2c811081a73f93) behind
// the nativemlow build tag. Only the ENCODER is native: the inbound path stays on
// the byte-exact Go decoder in internal/voip/codec/mlow, which owns RED/SID/TOC
// handling and is the wire-compat oracle the native frames are tested against.
package nativemlow

/*
#cgo LDFLAGS: -lopus -lm
#include <opus.h>

static int enc_ctl_int(OpusEncoder *st, int request, opus_int32 value) {
	return opus_encoder_ctl(st, request, value);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	sampleRate   = 16000
	frameSamples = 960
	maxPacket    = 4000

	// Pinned to the Go encoder's effective config (mlow/analysis.go:19,372-373:
	// MainBitRate 20000, PayloadSizeMs 60, FEC 0; complexity-8-equivalent paths).
	bitrate    = 20000
	complexity = 8
)

var (
	globalOnce   sync.Once
	liveEncoders atomic.Int64
)

// Encoder wraps one C OpusEncoder in SMPL mode. The mutex serializes Encode and
// Close: audio.Detach can call Close while the send loop's last Encode is still
// in flight, which is harmless on the Go codec but a use-after-free on C state.
type Encoder struct {
	mu      sync.Mutex
	st      *C.OpusEncoder
	cleanup runtime.Cleanup
}

func NewEncoder() (*Encoder, error) {
	globalOnce.Do(func() { C.opus_global_create() })
	var cerr C.int
	st := C.opus_encoder_create(sampleRate, 1, C.OPUS_APPLICATION_VOIP, &cerr)
	if cerr != C.OPUS_OK {
		return nil, fmt.Errorf("nativemlow: opus_encoder_create: %d", int(cerr))
	}
	for _, ctl := range [...]struct {
		req C.int
		val C.opus_int32
	}{
		{C.OPUS_SET_BITRATE_REQUEST, bitrate},
		{C.OPUS_SET_COMPLEXITY_REQUEST, complexity},
		{C.OPUS_SET_USE_SMPL_REQUEST, 1},
		// The send loop encodes a frame every 60 ms including all-zero PCM;
		// SMPL's own DTX/SID machine must never introduce cadence gaps.
		{C.OPUS_SET_DTX_REQUEST, 0},
	} {
		if rc := C.enc_ctl_int(st, ctl.req, ctl.val); rc != C.OPUS_OK {
			C.opus_encoder_destroy(st)
			return nil, fmt.Errorf("nativemlow: encoder ctl %d: %d", int(ctl.req), int(rc))
		}
	}
	e := &Encoder{st: st}
	liveEncoders.Add(1)
	// Backstop only: the composite codec's Close is the real lifecycle owner.
	e.cleanup = runtime.AddCleanup(e, func(st *C.OpusEncoder) {
		C.opus_encoder_destroy(st)
		liveEncoders.Add(-1)
	}, st)
	return e, nil
}

// Encode turns one 60 ms frame (exactly 960 samples) into a wire MLow frame,
// sanitizing like the Go encoder (NaN to 0, clamp [-1,1]).
func (e *Encoder) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) != frameSamples {
		return nil, errors.New("nativemlow: expected 960 samples (60 ms @16 kHz)")
	}
	clean := make([]float32, frameSamples)
	for i, s := range pcm {
		switch {
		case math.IsNaN(float64(s)):
			s = 0.0
		case s < -1.0:
			s = -1.0
		case s > 1.0:
			s = 1.0
		}
		clean[i] = s
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.st == nil {
		return nil, errors.New("nativemlow: encoder closed")
	}
	var buf [maxPacket]byte
	n := C.opus_encode_float(e.st,
		(*C.float)(unsafe.Pointer(&clean[0])), frameSamples,
		(*C.uchar)(unsafe.Pointer(&buf[0])), maxPacket)
	// Keeps e (and so the AddCleanup backstop) provably live across the C call,
	// independent of how the surrounding code is refactored.
	runtime.KeepAlive(e)
	if n < 0 {
		return nil, fmt.Errorf("nativemlow: opus_encode_float: %d", int(n))
	}
	out := make([]byte, int(n))
	copy(out, buf[:n])
	return out, nil
}

// Close destroys the C state. Safe to call more than once.
func (e *Encoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.st == nil {
		return
	}
	e.cleanup.Stop()
	C.opus_encoder_destroy(e.st)
	runtime.KeepAlive(e)
	e.st = nil
	liveEncoders.Add(-1)
}

// LiveEncoders reports undestroyed C encoders (test hook for leak assertions).
func LiveEncoders() int64 {
	return liveEncoders.Load()
}
