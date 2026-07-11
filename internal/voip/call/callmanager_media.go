package call

import (
	"context"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

const rtpSessionBytes = 8 * 1024

func (m *CallManager) replaceRtpSession(s *media.RtpSession) {
	if m.rtpSession != nil {
		m.observer.ReleaseMem(rtpSessionBytes)
	}
	m.rtpSession = s
	if s != nil {
		m.observer.AddMem(rtpSessionBytes)
	}
}

func (m *CallManager) FeedCapturedPCM(data []float32) {
	if a, ok := engine.Capability[core.AudioSink](m.extensions); ok {
		a.FeedPCM(data)
	}
}

// FlushCapturedPCM discards audio that has not yet been sent to the peer. The
// audio extension also invalidates a frame currently being encoded, so no
// stale TTS frame can be emitted after this method returns.
func (m *CallManager) FlushCapturedPCM() {
	if a, ok := engine.Capability[core.AudioSink](m.extensions); ok {
		a.FlushPCM()
	}
}

// WaitCapturedPCMDrained waits until every queued captured sample has been
// encoded and handed to the relay. It is used by the local fallback path to
// avoid tearing down the call before the warning has played.
func (m *CallManager) WaitCapturedPCMDrained(ctx context.Context) error {
	if a, ok := engine.Capability[core.AudioSink](m.extensions); ok {
		return a.WaitPCMDrained(ctx)
	}
	return nil
}

func (m *CallManager) registerRTPHandler(pt uint8, handler func(*media.RtpPacket)) {
	m.extMu.Lock()
	m.rtpHandlers[pt] = handler
	m.extMu.Unlock()
}

func (m *CallManager) declareSelfSSRC(ssrc uint32) {
	m.extMu.Lock()
	m.declaredSelf[ssrc] = true
	m.extMu.Unlock()
}

func (m *CallManager) ensureExtensionsAttachedLocked(ourDeviceJid, peerDeviceJid string) {
	if m.extAttached {
		return
	}
	m.extAttached = true
	scope := &engine.CallScope{
		Log:             m.log,
		CallID:          m.currentCall.CallID,
		OwnDeviceJID:    ourDeviceJid,
		PeerDeviceJID:   peerDeviceJid,
		IsVideo:         m.currentCall.MediaType == core.CallMediaTypeVideo,
		Relay:           m.relay,
		SendAudioFrame:  m.sendAudioFrame,
		SendRTP:         m.sendRTP,
		OnRTP:           m.registerRTPHandler,
		DeclareSelfSSRC: m.declareSelfSSRC,
		Observer:        m.observer,
	}
	for _, e := range m.extensions {
		if err := e.Attach(scope); err != nil {
			m.log.Error("extension attach failed", "ext", e.Name(), "err", err)
		}
	}
	if a, ok := engine.Capability[core.AudioSink](m.extensions); ok {
		a.OnPeerPCM(func(pcm []float32) {
			if m.OnPeerAudio != nil {
				m.OnPeerAudio(pcm)
			}
		})
	}
	if v, ok := engine.Capability[core.VideoSink](m.extensions); ok {
		v.OnPeerAU(func(au []byte) {
			if m.OnPeerVideo != nil {
				m.OnPeerVideo(au)
			}
		})
	}
}

func (m *CallManager) sendAudioFrame(encoded []byte, frameSamples int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rtpSession == nil || m.srtp == nil {
		return nil
	}
	marker := !m.firstPacketSent
	if marker {
		m.observer.Mark("media.first_packet")
	}
	pkt := m.rtpSession.CreatePacketWithDuration(encoded, frameSamples, marker)
	if m.debeEnabled {
		pkt.Header.Extension = true
		pkt.Header.ExtensionProfile = 0xbede
		pkt.Header.ExtensionData = nil
	}
	m.firstPacketSent = true
	protected, err := m.srtp.Protect(pkt)
	if err != nil {
		m.log.Debug("srtp protect error", "err", err)
		return err
	}
	m.relay.Broadcast(protected)
	return nil
}

func (m *CallManager) sendRTP(pkt *media.RtpPacket) error {
	m.mu.Lock()
	srtp := m.srtp
	m.mu.Unlock()
	if srtp == nil {
		return nil
	}
	protected, err := srtp.Protect(pkt)
	if err != nil {
		return err
	}
	m.relay.Broadcast(protected)
	return nil
}

func (m *CallManager) onRelayData(data []byte) {
	if transport.IsStunPacket(data) {
		return
	}
	if !transport.IsRtpPacket(data) {
		return
	}
	if len(data) < 12 {
		return
	}
	pt := data[1] & 0x7f
	ssrc := media.RTPSsrc(data)

	m.mu.Lock()
	if ssrc == m.selfSsrc {
		m.mu.Unlock()
		return
	}
	if pt == core.PayloadTypeWhatsAppOpus && !m.actualPeerSet {
		m.actualPeerSet = true
		if !containsSsrc(m.peerSsrcs, ssrc) {
			m.peerSsrcs = []uint32{ssrc}
			m.relay.SetSubscriptionSsrc(ssrc)
			go m.relay.ResendSubscriptions()
		}
	}
	srtp := m.srtp
	m.mu.Unlock()

	m.extMu.Lock()
	skip := m.declaredSelf[ssrc]
	handler := m.rtpHandlers[pt]
	m.extMu.Unlock()
	if skip || srtp == nil || handler == nil {
		return
	}

	pkt, err := srtp.Unprotect(data)
	if err != nil {
		m.log.Debug("srtp unprotect error", "err", err)
		return
	}
	if len(pkt.Payload) == 0 {
		return
	}
	handler(pkt)
}
