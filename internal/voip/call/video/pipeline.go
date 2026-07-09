package video

import (
	"log/slog"
	"sync"
	"time"

	"wacalls/internal/voip/codec/h264"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

const (
	rtpStepSamples      = 90000 / 15
	congestionDropBytes = 48 * 1024
	slotWord            = 2
)

var (
	callSlots       = []uint32{0, 1, 4, 2, 3, 5}
	annexBStartCode = []byte{0, 0, 0, 1}
)

type Relay interface {
	Broadcast(data []byte)
	BufferedAmount() uint64
	HasConnection() bool
	SetStreamSsrcs(selfSsrcs, peerSsrcs []uint32)
}

type Pipeline struct {
	log   *slog.Logger
	relay Relay

	mu       sync.Mutex
	rtp      *media.RtpSession
	srtp     *engine.SrtpManager
	selfSsrc uint32
	depack   *h264.H264Depacketizer
	frameBuf []byte
	lastAUAt time.Time

	OnFrame func(au []byte)
}

func New(log *slog.Logger, relay Relay) *Pipeline {
	if log == nil {
		log = slog.Default()
	}
	return &Pipeline{log: log, relay: relay}
}

func (p *Pipeline) Setup(callID, ourDeviceJid, peerDeviceJid string, srtp *engine.SrtpManager) error {
	selfSsrc := media.GenerateSecureSsrc(callID, ourDeviceJid, slotWord)

	selfSsrcs := make([]uint32, len(callSlots))
	peerSsrcs := make([]uint32, len(callSlots))
	for i, slot := range callSlots {
		selfSsrcs[i] = media.GenerateSecureSsrc(callID, ourDeviceJid, slot)
		peerSsrcs[i] = media.GenerateSecureSsrc(callID, peerDeviceJid, slot)
	}

	p.mu.Lock()
	p.srtp = srtp
	p.selfSsrc = selfSsrc
	p.rtp = h264.NewSession(selfSsrc)
	if p.depack == nil {
		p.depack = &h264.H264Depacketizer{}
	}
	p.mu.Unlock()

	p.relay.SetStreamSsrcs(selfSsrcs, peerSsrcs)
	p.log.Debug("video media set up", "self_video_ssrc", selfSsrc,
		"stream_ssrcs", len(selfSsrcs)+len(peerSsrcs))
	return nil
}

func (p *Pipeline) FeedCaptured(au []byte) {
	p.mu.Lock()
	rtp, srtp := p.rtp, p.srtp
	p.mu.Unlock()
	if rtp == nil || srtp == nil || !p.relay.HasConnection() || len(au) == 0 {
		return
	}
	nalus := h264.SplitAnnexB(au)
	if len(nalus) == 0 {
		return
	}
	if p.relay.BufferedAmount() > congestionDropBytes {
		return
	}
	var payloads [][]byte
	for _, nalu := range nalus {
		payloads = append(payloads, h264.PackageH264NALU(nalu)...)
	}

	p.mu.Lock()
	first := p.lastAUAt.IsZero()
	p.lastAUAt = time.Now()
	p.mu.Unlock()
	if !first {
		rtp.AdvanceTimestamp(rtpStepSamples)
	}
	for i, payload := range payloads {
		last := i == len(payloads)-1
		pkt := rtp.CreatePacketWithDuration(payload, 0, last)
		protected, err := srtp.Protect(pkt)
		if err != nil {
			continue
		}
		p.relay.Broadcast(protected)
	}
}

func (p *Pipeline) HandleRelayData(data []byte) {
	p.mu.Lock()
	srtp, depack, selfSsrc := p.srtp, p.depack, p.selfSsrc
	p.mu.Unlock()
	if srtp == nil || depack == nil {
		return
	}
	if media.RTPSsrc(data) == selfSsrc {
		return
	}

	pkt, err := srtp.Unprotect(data)
	if err != nil {
		p.log.Debug("video srtp unprotect error", "err", err)
		return
	}
	if len(pkt.Payload) == 0 {
		return
	}
	nalus := depack.Depacketize(pkt.Payload)

	p.mu.Lock()
	for _, nalu := range nalus {
		p.frameBuf = append(p.frameBuf, annexBStartCode...)
		p.frameBuf = append(p.frameBuf, nalu...)
	}
	var frame []byte
	if pkt.Header.Marker && len(p.frameBuf) > 0 {
		frame = p.frameBuf
		p.frameBuf = nil
	}
	cb := p.OnFrame
	p.mu.Unlock()

	if frame != nil && cb != nil {
		cb(frame)
	}
}

func (p *Pipeline) Reset() {
	p.mu.Lock()
	p.rtp = nil
	p.srtp = nil
	p.selfSsrc = 0
	p.depack = nil
	p.frameBuf = nil
	p.lastAUAt = time.Time{}
	p.mu.Unlock()
}
