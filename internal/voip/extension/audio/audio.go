package audio

import (
	"context"
	"runtime/pprof"
	"sync"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

const audioCodecBytes = 96 * 1024

// jitterDepth is how many newer packets must pile up behind a missing one before it
// is concealed, giving a reordered packet time to arrive. 3 packets at 60 ms is 180 ms.
const jitterDepth = 3

type Audio struct {
	codec        core.AudioCodec
	scope        *engine.CallScope
	mu           sync.Mutex
	captureBuf   []float32
	sendLoopStop chan struct{}
	onPeerPCM    func([]float32)
	detached     bool

	jitter    *jitterBuffer
	lastFrame []float32
	concealed int
}

func New(codec core.AudioCodec) *Audio {
	return &Audio{codec: codec, jitter: newJitterBuffer(jitterDepth)}
}

func (a *Audio) Name() string {
	return "audio"
}

func (a *Audio) Attach(scope *engine.CallScope) error {
	a.mu.Lock()
	a.scope = scope
	scope.Observer.AddMem(audioCodecBytes)
	a.startSendLoopLocked()
	a.mu.Unlock()
	scope.OnRTP(core.PayloadTypeWhatsAppOpus, a.handleInbound)
	return nil
}

func (a *Audio) Detach() {
	a.mu.Lock()
	if a.detached {
		a.mu.Unlock()
		return
	}
	a.detached = true
	if a.sendLoopStop != nil {
		close(a.sendLoopStop)
		a.sendLoopStop = nil
	}
	scope := a.scope
	a.mu.Unlock()
	if scope != nil {
		scope.Observer.ReleaseMem(audioCodecBytes)
	}
	a.codec.Close()
}

func (a *Audio) FeedPCM(pcm []float32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.codec == nil || len(pcm) == 0 {
		return
	}
	a.captureBuf = append(a.captureBuf, pcm...)
	if maxBuffered := a.codec.FrameSize() * 4; len(a.captureBuf) > maxBuffered {
		a.captureBuf = a.captureBuf[len(a.captureBuf)-maxBuffered:]
	}
}

func (a *Audio) OnPeerPCM(cb func([]float32)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onPeerPCM = cb
}

func (a *Audio) startSendLoopLocked() {
	if a.sendLoopStop != nil || a.codec == nil {
		return
	}
	stop := make(chan struct{})
	a.sendLoopStop = stop
	frameSize := a.codec.FrameSize()
	done := a.scope.Observer.TrackGoroutine()
	callID := a.scope.CallID
	go func() {
		pprof.SetGoroutineLabels(pprof.WithLabels(context.Background(), pprof.Labels("call_id", callID)))
		defer done()
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()
		silence := make([]float32, frameSize)
		voiced := make([]float32, frameSize)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			a.mu.Lock()
			if a.scope == nil || a.codec == nil || a.scope.Relay == nil || !a.scope.Relay.HasConnection() {
				a.mu.Unlock()
				continue
			}
			frame := silence
			if len(a.captureBuf) >= frameSize {
				copy(voiced, a.captureBuf[:frameSize])
				frame = voiced
				a.captureBuf = a.captureBuf[frameSize:]
			}
			scope := a.scope
			codec := a.codec
			a.mu.Unlock()
			if enc, err := codec.Encode(frame); err == nil {
				_ = scope.SendAudioFrame(enc, codec.FrameSize())
			}
		}
	}()
}

// handleInbound feeds the packet through the jitter buffer and plays out whatever it
// releases in sequence order: present frames are decoded (in order, so the stateful
// codec stays coherent) and missing ones are concealed.
func (a *Audio) handleInbound(pkt *media.RtpPacket) {
	a.mu.Lock()
	frames := a.jitter.push(pkt.Header.SequenceNumber, pkt.Payload)
	var out [][]float32
	for _, fr := range frames {
		if fr.present {
			pcm, err := a.codec.Decode(fr.payload)
			if err != nil || len(pcm) == 0 {
				continue
			}
			a.lastFrame = pcm
			a.concealed = 0
			out = append(out, pcm)
		} else if concealed := a.concealLocked(); concealed != nil {
			out = append(out, concealed)
		}
	}
	cb := a.onPeerPCM
	a.mu.Unlock()
	if cb != nil {
		for _, pcm := range out {
			cb(pcm)
		}
	}
}

// concealLocked produces a frame to cover a lost packet: the last decoded frame faded
// out once, then silence for any consecutive losses. Caller holds a.mu.
func (a *Audio) concealLocked() []float32 {
	n := a.codec.FrameSize()
	if len(a.lastFrame) > 0 {
		n = len(a.lastFrame)
	}
	pcm := make([]float32, n)
	if a.concealed == 0 && len(a.lastFrame) > 1 {
		last := len(pcm) - 1
		for i := range pcm {
			pcm[i] = a.lastFrame[i] * (1 - float32(i)/float32(last))
		}
	}
	a.concealed++
	return pcm
}

var _ core.AudioSink = (*Audio)(nil)
var _ engine.Extension = (*Audio)(nil)
