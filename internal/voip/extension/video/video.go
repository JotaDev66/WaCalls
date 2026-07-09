package video

import (
	"sync"
	"time"

	"wacalls/internal/voip/codec/h264"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

const (
	rtpStepSamples             = 90000 / 15
	congestionDropBytes        = 48 * 1024
	slotWord            uint32 = 2
)

var (
	callSlots       = []uint32{0, 1, 4, 2, 3, 5}
	annexBStartCode = []byte{0, 0, 0, 1}
)

type Video struct {
	scope    *engine.CallScope
	mu       sync.Mutex
	rtp      *media.RtpSession
	selfSsrc uint32
	depack   *h264.H264Depacketizer
	frameBuf []byte
	lastAUAt time.Time
	onPeerAU func([]byte)
}

func New() *Video {
	return &Video{}
}

func (v *Video) Name() string {
	return "video"
}

func (v *Video) Attach(scope *engine.CallScope) error {
	if !scope.IsVideo {
		return nil
	}
	selfSsrc := media.GenerateSecureSsrc(scope.CallID, scope.OwnDeviceJID, slotWord)

	selfSsrcs := make([]uint32, len(callSlots))
	peerSsrcs := make([]uint32, len(callSlots))
	for i, slot := range callSlots {
		selfSsrcs[i] = media.GenerateSecureSsrc(scope.CallID, scope.OwnDeviceJID, slot)
		peerSsrcs[i] = media.GenerateSecureSsrc(scope.CallID, scope.PeerDeviceJID, slot)
	}

	v.mu.Lock()
	v.scope = scope
	v.selfSsrc = selfSsrc
	v.rtp = h264.NewSession(selfSsrc)
	v.depack = &h264.H264Depacketizer{}
	v.mu.Unlock()

	scope.Relay.SetStreamSsrcs(selfSsrcs, peerSsrcs)
	scope.DeclareSelfSSRC(selfSsrc)
	scope.OnRTP(core.PayloadTypeWhatsAppH264, v.handleInbound)
	return nil
}

func (v *Video) Detach() {
	v.mu.Lock()
	v.rtp = nil
	v.depack = nil
	v.frameBuf = nil
	v.selfSsrc = 0
	v.lastAUAt = time.Time{}
	v.mu.Unlock()
}

func (v *Video) FeedAU(au []byte) {
	v.mu.Lock()
	rtp, scope := v.rtp, v.scope
	v.mu.Unlock()
	if rtp == nil || scope == nil || !scope.Relay.HasConnection() || len(au) == 0 {
		return
	}
	nalus := h264.SplitAnnexB(au)
	if len(nalus) == 0 {
		return
	}
	if scope.Relay.BufferedAmount() > congestionDropBytes {
		return
	}
	var payloads [][]byte
	for _, nalu := range nalus {
		payloads = append(payloads, h264.PackageH264NALU(nalu)...)
	}

	v.mu.Lock()
	first := v.lastAUAt.IsZero()
	v.lastAUAt = time.Now()
	v.mu.Unlock()
	if !first {
		rtp.AdvanceTimestamp(rtpStepSamples)
	}
	for i, payload := range payloads {
		last := i == len(payloads)-1
		pkt := rtp.CreatePacketWithDuration(payload, 0, last)
		scope.SendRTP(pkt)
	}
}

func (v *Video) handleInbound(pkt *media.RtpPacket) {
	v.mu.Lock()
	depack := v.depack
	v.mu.Unlock()
	if depack == nil {
		return
	}
	nalus := depack.Depacketize(pkt.Payload)

	v.mu.Lock()
	for _, nalu := range nalus {
		v.frameBuf = append(v.frameBuf, annexBStartCode...)
		v.frameBuf = append(v.frameBuf, nalu...)
	}
	var frame []byte
	if pkt.Header.Marker && len(v.frameBuf) > 0 {
		frame = v.frameBuf
		v.frameBuf = nil
	}
	cb := v.onPeerAU
	v.mu.Unlock()

	if frame != nil && cb != nil {
		cb(frame)
	}
}

func (v *Video) OnPeerAU(cb func([]byte)) {
	v.mu.Lock()
	v.onPeerAU = cb
	v.mu.Unlock()
}

var (
	_ core.VideoSink   = (*Video)(nil)
	_ engine.Extension = (*Video)(nil)
)
