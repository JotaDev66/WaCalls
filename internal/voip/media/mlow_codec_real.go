//go:build mlow

package media

/*
#cgo CFLAGS: -I${SRCDIR}/../../../native
#cgo LDFLAGS: -L${SRCDIR}/../../../native -lopus_mlow

#include <stdint.h>
#include <stdlib.h>

// Prototypes for the bundled libopus_mlow (WhatsApp's Opus variant). The
// real library exports the standard opus_* symbols.
extern void  opus_global_create(void);
extern void* opus_decoder_create(int32_t fs, int channels, unsigned char* err);
extern int   opus_decoder_ctl(void* dec, int request, ...);
extern int   opus_decode(void* dec, const unsigned char* data, int32_t len, int16_t* pcm, int frame_size, int decode_fec);
extern void  opus_decoder_destroy(void* dec);
extern void* opus_encoder_create(int32_t fs, int channels, int application, unsigned char* err);
extern int   opus_encoder_ctl(void* enc, int request, ...);
extern int   opus_encode(void* enc, const int16_t* pcm, int frame_size, unsigned char* data, int32_t max_data_bytes);
extern void  opus_encoder_destroy(void* enc);
extern const char* opus_strerror(int error);

// Non-variadic wrappers so cgo can call the *_ctl functions with one int arg.
static int mlow_dec_ctl(void* dec, int req, int val) { return opus_decoder_ctl(dec, req, val); }
static int mlow_enc_ctl(void* enc, int req, int val) { return opus_encoder_ctl(enc, req, val); }
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

const (
	mlowChannels        = 1
	opusApplicationVOIP = 2048

	ctlSetBitrate    = 4002
	ctlSetComplexity = 4010
	ctlSetSignal     = 4024
	ctlSetInbandFEC  = 4012
	ctlSetDTX        = 4016
	ctlSetUsingSmpl  = 4050
	ctlSignalVoice   = 3001

	mlowMaxOut = 5760
)

var globalInitOnce sync.Once

type mlowCodec struct {
	encoder unsafe.Pointer
	decoder unsafe.Pointer
}

func NewMLowCodec(opts CodecOptions) (Codec, error) {
	// MLow is pure-Go now (newNativeMLow). The cgo path in this file remains only
	// for NewOpusCodec (browser-side Opus 48 kHz), pending the pure-Go migration.
	return newNativeMLow(), nil
}

func (c *mlowCodec) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, nil
	}
	in := make([]C.int16_t, len(pcm))
	for i, s := range pcm {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		in[i] = C.int16_t(int16(s * 32767))
	}
	out := make([]C.uchar, 4000)
	n := C.opus_encode(c.encoder, &in[0], C.int(len(pcm)), &out[0], C.int32_t(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("encode failed: %s", C.GoString(C.opus_strerror(n)))
	}
	res := make([]byte, int(n))
	for i := 0; i < int(n); i++ {
		res[i] = byte(out[i])
	}
	return res, nil
}

func (c *mlowCodec) Decode(frame []byte) ([]float32, error) {
	out := make([]C.int16_t, mlowMaxOut)
	var n C.int
	if frame == nil {
		n = C.opus_decode(c.decoder, nil, 0, &out[0], C.int(mlowFrameSize), 0)
	} else {
		cdata := (*C.uchar)(unsafe.Pointer(&frame[0]))
		n = C.opus_decode(c.decoder, cdata, C.int32_t(len(frame)), &out[0], C.int(mlowMaxOut), 0)
	}
	if n <= 0 {
		return make([]float32, mlowFrameSize), nil
	}
	res := make([]float32, int(n))
	for i := 0; i < int(n); i++ {
		res[i] = float32(int16(out[i])) / 32768.0
	}
	return res, nil
}

func (c *mlowCodec) FrameSize() int  { return mlowFrameSize }
func (c *mlowCodec) SampleRate() int { return mlowSampleRate }

func (c *mlowCodec) Close() {
	if c.decoder != nil {
		C.opus_decoder_destroy(c.decoder)
		c.decoder = nil
	}
	if c.encoder != nil {
		C.opus_encoder_destroy(c.encoder)
		c.encoder = nil
	}
}

type opusGeneric struct {
	encoder    unsafe.Pointer
	decoder    unsafe.Pointer
	sampleRate int
	frameSize  int
}

func NewOpusCodec(sampleRate, frameSize int) (Codec, error) {
	globalInitOnce.Do(func() { C.opus_global_create() })
	c := &opusGeneric{sampleRate: sampleRate, frameSize: frameSize}

	var errBuf [4]C.uchar
	c.decoder = C.opus_decoder_create(C.int32_t(sampleRate), C.int(1), &errBuf[0])
	if c.decoder == nil {
		return nil, fmt.Errorf("opus_decoder_create(%d) failed", sampleRate)
	}
	c.encoder = C.opus_encoder_create(C.int32_t(sampleRate), C.int(1), C.int(opusApplicationVOIP), &errBuf[0])
	if c.encoder == nil {
		C.opus_decoder_destroy(c.decoder)
		return nil, fmt.Errorf("opus_encoder_create(%d) failed", sampleRate)
	}
	C.mlow_enc_ctl(c.encoder, C.int(ctlSetBitrate), C.int(24000))
	C.mlow_enc_ctl(c.encoder, C.int(ctlSetComplexity), C.int(5))
	C.mlow_enc_ctl(c.encoder, C.int(ctlSetSignal), C.int(ctlSignalVoice))
	return c, nil
}

func (c *opusGeneric) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, nil
	}
	in := make([]C.int16_t, len(pcm))
	for i, s := range pcm {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		in[i] = C.int16_t(int16(s * 32767))
	}
	out := make([]C.uchar, 4000)
	n := C.opus_encode(c.encoder, &in[0], C.int(len(pcm)), &out[0], C.int32_t(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("opus encode failed: %s", C.GoString(C.opus_strerror(n)))
	}
	res := make([]byte, int(n))
	for i := 0; i < int(n); i++ {
		res[i] = byte(out[i])
	}
	return res, nil
}

func (c *opusGeneric) Decode(frame []byte) ([]float32, error) {
	maxOut := c.sampleRate / 1000 * 120
	out := make([]C.int16_t, maxOut)
	var n C.int
	if frame == nil {
		n = C.opus_decode(c.decoder, nil, 0, &out[0], C.int(c.frameSize), 0)
	} else {
		cdata := (*C.uchar)(unsafe.Pointer(&frame[0]))
		n = C.opus_decode(c.decoder, cdata, C.int32_t(len(frame)), &out[0], C.int(maxOut), 0)
	}
	if n <= 0 {
		return make([]float32, c.frameSize), nil
	}
	res := make([]float32, int(n))
	for i := 0; i < int(n); i++ {
		res[i] = float32(int16(out[i])) / 32768.0
	}
	return res, nil
}

func (c *opusGeneric) FrameSize() int  { return c.frameSize }
func (c *opusGeneric) SampleRate() int { return c.sampleRate }

func (c *opusGeneric) Close() {
	if c.decoder != nil {
		C.opus_decoder_destroy(c.decoder)
		c.decoder = nil
	}
	if c.encoder != nil {
		C.opus_encoder_destroy(c.encoder)
		c.encoder = nil
	}
}
