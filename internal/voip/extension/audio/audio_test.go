package audio

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

func TestAudioDecodesInbound(t *testing.T) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	a := New(codec)
	var got []float32
	a.OnPeerPCM(func(p []float32) { got = p })

	frame := make([]float32, codec.FrameSize())
	for i := range frame {
		frame[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	enc, err := codec.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	pkt := &media.RtpPacket{
		Header:  media.NewRtpHeader(core.PayloadTypeWhatsAppOpus, 1, 0, 5000),
		Payload: enc,
	}
	a.handleInbound(pkt)

	if len(got) == 0 {
		t.Fatal("expected decoded peer PCM, got none")
	}
}

func TestAudioFeedPCMBounds(t *testing.T) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	a := New(codec)
	a.FeedPCM(make([]float32, codec.FrameSize()*10))

	if len(a.captureBuf) > codec.FrameSize()*4 {
		t.Fatalf("captureBuf=%d exceeds bound %d", len(a.captureBuf), codec.FrameSize()*4)
	}
}

func TestAudioFlushClearsCaptureBufferAndUnblocksDrain(t *testing.T) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	a := New(codec)
	a.FeedPCM(make([]float32, codec.FrameSize()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	drained := make(chan error, 1)
	go func() { drained <- a.WaitPCMDrained(ctx) }()
	a.FlushPCM()

	if err := <-drained; err != nil {
		t.Fatalf("WaitPCMDrained: %v", err)
	}
	if len(a.captureBuf) != 0 {
		t.Fatalf("capture buffer has %d samples after flush", len(a.captureBuf))
	}
}

func TestAudioFlushInvalidatesFrameBeingEncoded(t *testing.T) {
	codec := &blockingAudioCodec{started: make(chan struct{}), release: make(chan struct{})}
	relay := &toggleRelay{}
	relay.connected.Store(true)
	sent := make(chan struct{}, 1)
	a := New(codec)
	a.FeedPCM(make([]float32, codec.FrameSize()))
	if err := a.Attach(&engine.CallScope{
		CallID: "flush-test", Relay: relay, Observer: core.NopObserver{},
		SendAudioFrame: func([]byte, int) error {
			sent <- struct{}{}
			return nil
		},
		OnRTP: func(uint8, func(*media.RtpPacket)) {},
	}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer a.Detach()

	select {
	case <-codec.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for encode")
	}
	a.FlushPCM()
	relay.connected.Store(false)
	close(codec.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.WaitPCMDrained(ctx); err != nil {
		t.Fatalf("WaitPCMDrained: %v", err)
	}
	select {
	case <-sent:
		t.Fatal("flushed in-flight frame was sent")
	default:
	}
}

type blockingAudioCodec struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingAudioCodec) Encode([]float32) ([]byte, error) {
	close(c.started)
	<-c.release
	return []byte{1}, nil
}
func (*blockingAudioCodec) Decode([]byte) ([]float32, error) { return nil, nil }
func (*blockingAudioCodec) FrameSize() int                   { return 960 }
func (*blockingAudioCodec) SampleRate() int                  { return 16_000 }
func (*blockingAudioCodec) Close()                           {}

type toggleRelay struct {
	connected atomic.Bool
}

func (*toggleRelay) Broadcast([]byte)                  {}
func (*toggleRelay) BufferedAmount() uint64            { return 0 }
func (r *toggleRelay) HasConnection() bool             { return r.connected.Load() }
func (*toggleRelay) SetStreamSsrcs([]uint32, []uint32) {}
