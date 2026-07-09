package call

import (
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/extension/audio"
	"wacalls/internal/voip/media"
)

type countingObserver struct {
	mu           sync.Mutex
	mem, memPeak int64
	gor, gorPeak int64
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
func (o *countingObserver) Mark(string)        {}
func (o *countingObserver) End(string, string) {}
func (o *countingObserver) memNow() int64      { o.mu.Lock(); defer o.mu.Unlock(); return o.mem }
func (o *countingObserver) gorNow() int64      { o.mu.Lock(); defer o.mu.Unlock(); return o.gor }
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
	frame := make([]float32, codec.FrameSize())
	enc, err := codec.Encode(frame)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := cm.sendAudioFrame(enc, codec.FrameSize()); err != nil {
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
