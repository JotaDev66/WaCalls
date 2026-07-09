package audio

import (
	"sync"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

type Audio struct {
	codec              core.AudioCodec
	scope              *engine.CallScope
	mu                 sync.Mutex
	captureBuf         []float32
	audioTimelineSet   bool
	audioBaseTs        uint32
	audioPlayedSamples uint64
	sendLoopStop       chan struct{}
	onPeerPCM          func([]float32)
}

func New(codec core.AudioCodec) *Audio {
	return &Audio{codec: codec}
}

func (a *Audio) Name() string {
	return "audio"
}

func (a *Audio) Attach(scope *engine.CallScope) error {
	a.mu.Lock()
	a.scope = scope
	a.startSendLoopLocked()
	a.mu.Unlock()
	scope.OnRTP(core.PayloadTypeWhatsAppOpus, a.handleInbound)
	return nil
}

func (a *Audio) Detach() {
	a.mu.Lock()
	if a.sendLoopStop != nil {
		close(a.sendLoopStop)
		a.sendLoopStop = nil
	}
	a.mu.Unlock()
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
	go func() {
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()
		silence := make([]float32, frameSize)
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
				frame = make([]float32, frameSize)
				copy(frame, a.captureBuf[:frameSize])
				a.captureBuf = a.captureBuf[frameSize:]
			}
			scope := a.scope
			codec := a.codec
			a.mu.Unlock()
			if enc, err := codec.Encode(frame); err == nil {
				scope.SendAudioFrame(enc, codec.FrameSize())
			}
		}
	}()
}

func (a *Audio) handleInbound(pkt *media.RtpPacket) {
	pcm, err := a.codec.Decode(pkt.Payload)
	if err != nil || len(pcm) == 0 {
		return
	}
	aligned := a.align(pkt.Header.Timestamp, pcm)
	a.mu.Lock()
	cb := a.onPeerPCM
	a.mu.Unlock()
	if cb != nil {
		cb(aligned)
	}
}

func (a *Audio) align(ts uint32, pcm []float32) []float32 {
	const maxGapSamples = 8000
	a.mu.Lock()
	defer a.mu.Unlock()
	origLen := uint64(len(pcm))
	if !a.audioTimelineSet {
		a.audioTimelineSet = true
		a.audioBaseTs = ts
		a.audioPlayedSamples = origLen
		return pcm
	}
	target := uint64(ts - a.audioBaseTs)
	gap := int64(target) - int64(a.audioPlayedSamples)
	if gap < 0 || gap > maxGapSamples {
		a.audioBaseTs = ts
		a.audioPlayedSamples = origLen
		return pcm
	}
	if gap > 0 {
		padded := make([]float32, int(gap)+int(origLen))
		copy(padded[int(gap):], pcm)
		pcm = padded
	}
	a.audioPlayedSamples = target + origLen
	return pcm
}

var _ core.AudioSink = (*Audio)(nil)
var _ engine.Extension = (*Audio)(nil)
