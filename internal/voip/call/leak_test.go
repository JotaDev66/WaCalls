package call

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/extension/audio"
	"wacalls/internal/voip/media"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type countingObserver struct {
	mu           sync.Mutex
	mem, memPeak int64
	gor, gorPeak int64
	quality      int64
}

func (o *countingObserver) AddMem(b int64) {
	o.mu.Lock()
	o.mem += b
	if o.mem > o.memPeak {
		o.memPeak = o.mem
	}
	o.mu.Unlock()
}
func (o *countingObserver) ReleaseMem(b int64) { o.mu.Lock(); o.mem -= b; o.mu.Unlock() }
func (o *countingObserver) TrackGoroutine() func() {
	o.mu.Lock()
	o.gor++
	if o.gor > o.gorPeak {
		o.gorPeak = o.gor
	}
	o.mu.Unlock()
	return func() { o.mu.Lock(); o.gor--; o.mu.Unlock() }
}
func (o *countingObserver) Mark(string)                  {}
func (o *countingObserver) SrtpRecvDrop(string)          {}
func (o *countingObserver) NoteQuality(core.CallQuality) { o.mu.Lock(); o.quality++; o.mu.Unlock() }
func (o *countingObserver) End(string, string)           {}
func (o *countingObserver) memNow() int64                { o.mu.Lock(); defer o.mu.Unlock(); return o.mem }
func (o *countingObserver) gorNow() int64                { o.mu.Lock(); defer o.mu.Unlock(); return o.gor }
func (o *countingObserver) qualityNow() int64            { o.mu.Lock(); defer o.mu.Unlock(); return o.quality }
func (o *countingObserver) goroutinePeak() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.gorPeak
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", d)
	}
}

type closeCountCodec struct{ closed atomic.Int32 }

func (c *closeCountCodec) Encode([]float32) ([]byte, error) { return []byte{0}, nil }
func (c *closeCountCodec) Decode([]byte) ([]float32, error) { return nil, nil }
func (c *closeCountCodec) FrameSize() int                   { return 960 }
func (c *closeCountCodec) SampleRate() int                  { return 16000 }
func (c *closeCountCodec) Close()                           { c.closed.Add(1) }

type failingQuerySock struct{ fakeSock }

func (failingQuerySock) Query(context.Context, waBinary.Node) (*waBinary.Node, error) {
	return nil, errors.New("network down")
}

// TestFailedStartCallClosesCodec: a dial that errors before setup must still tear
// down media so every extension's codec.Close fires (a real resource once the
// native encoder lands; silently leaked before this test existed).
func TestFailedStartCallClosesCodec(t *testing.T) {
	codec := &closeCountCodec{}
	client := NewClient(failingQuerySock{}, slog.Default(), func() []engine.Extension {
		return []engine.Extension{audio.New(codec)}
	}, 0, func(string, *CallManager) {}, nil)

	_, err := client.StartCall(context.Background(), types.NewJID("5511999999999", types.DefaultUserServer))
	if err == nil {
		t.Fatal("expected StartCall to fail")
	}
	if codec.closed.Load() == 0 {
		t.Fatal("codec.Close not called after failed StartCall")
	}
	if n := client.Count(); n != 0 {
		t.Fatalf("call not removed after failed StartCall: %d", n)
	}
}

func TestCallLifecycleNoLeak(t *testing.T) {
	obs := &countingObserver{}
	baseGor := runtime.NumGoroutine()

	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	cm := NewCallManager(fakeSock{}, slog.Default(), audio.New(codec))
	cm.observer = obs
	cm.relay = &fakeRelay{}
	cm.srtp = engine.NewSrtpManager(km(1), km(9), core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	cm.srtp.SetObserver(obs)
	cm.selfSsrc = 1000
	cm.rtpSession = media.NewWhatsAppOpusSession(1000)
	obs.AddMem(rtpSessionBytes) // mirror the StartCall accounting this direct test bypasses
	cm.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)

	cm.ensureExtensionsAttachedLocked("our.0", "peer.0") // attach: audio goroutine + codec mem

	if obs.goroutinePeak() == 0 {
		t.Fatal("no goroutine tracked on attach; audio send-loop TrackGoroutine is missing")
	}

	memBeforeSend := obs.memNow()
	encCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("enc codec: %v", err)
	}
	frameSize := encCodec.FrameSize()
	enc, err := encCodec.Encode(make([]float32, frameSize))
	encCodec.Close()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := cm.sendAudioFrame(enc, frameSize); err != nil {
		t.Fatalf("sendAudioFrame: %v", err)
	}
	if obs.memNow() <= memBeforeSend {
		t.Fatal("SRTP context memory not accounted on first send; AddMem is missing")
	}

	cm.cleanupMedia() // End: detach (async goroutine stop) + srtp/rtpSession release

	waitFor(t, 2*time.Second, func() bool { return obs.gorNow() == 0 })
	if m := obs.memNow(); m != 0 {
		t.Fatalf("mem leak: %d bytes tracked but not released after teardown", m)
	}
	waitFor(t, 2*time.Second, func() bool { return runtime.NumGoroutine() <= baseGor })
	if g := runtime.NumGoroutine(); g > baseGor {
		t.Fatalf("runtime goroutine leak: baseline %d, after teardown %d", baseGor, g)
	}
}
