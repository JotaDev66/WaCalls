package call

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/extension/audio"
	"wacalls/internal/voip/media"

	"go.mau.fi/whatsmeow/types"
)

func TestCallLifecycleE2EThroughClient(t *testing.T) {
	obs := &countingObserver{}
	baseGor := runtime.NumGoroutine()
	ctx := context.Background()
	peer := types.NewJID("5511999990000", types.DefaultUserServer)

	c := NewClient(fakeSock{}, slog.Default(),
		func() []engine.Extension {
			codec, _ := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
			return []engine.Extension{audio.New(codec)}
		},
		0,
		func(string, *CallManager) {},
		func(string) core.CallObserver { return obs },
	)

	c.HandleOffer(ctx, offerNode("CALL1", peer), peer)
	if c.Count() != 1 {
		t.Fatalf("offer must register one call, count=%d", c.Count())
	}
	recv, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("call must be registered")
	}

	k1, k2 := km(1), km(9)
	recv.relay = &fakeRelay{}
	recv.srtp = engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	recv.selfSsrc = 2000
	var got []float32
	recv.OnPeerAudio = func(pcm []float32) { got = pcm }
	recv.ensureExtensionsAttachedLocked("our.0", "peer.0")

	send := NewCallManager(fakeSock{}, slog.Default())
	send.relay = &fakeRelay{onData: recv.onRelayData}
	send.srtp = engine.NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	send.rtpSession = media.NewWhatsAppOpusSession(1000)
	send.selfSsrc = 1000

	sendCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("send codec: %v", err)
	}
	enc, err := sendCodec.Encode(make([]float32, sendCodec.FrameSize()))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := send.sendAudioFrame(enc, sendCodec.FrameSize()); err != nil {
		t.Fatalf("sendAudioFrame: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("media did not flow through the registered call")
	}

	c.HandleTerminate(offerNode("CALL1", peer))
	if s, _ := stateOf(recv); s != core.CallStateEnded {
		t.Fatalf("terminate must end the call, state=%s", s)
	}
	recv.cleanupMedia()
	c.Remove("CALL1")

	if c.Count() != 0 {
		t.Fatalf("call must be removed after terminate, count=%d", c.Count())
	}
	waitFor(t, 2*time.Second, func() bool { return obs.gorNow() == 0 })
	waitFor(t, 2*time.Second, func() bool { return runtime.NumGoroutine() <= baseGor })
}
