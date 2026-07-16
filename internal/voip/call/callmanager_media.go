package call

import (
	"errors"
	"sync"
	"time"

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
		Relay:           m.relay,
		SendAudioFrame:  m.sendAudioFrame,
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
}

func (m *CallManager) sendAudioFrame(encoded []byte, frameSamples int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rtpSession == nil || m.srtp == nil {
		return nil
	}
	marker := !m.firstPacketSent
	if marker {
		m.observer.Mark(core.MarkMediaFirstPacket)
	}
	pkt := m.rtpSession.CreatePacketWithDuration(encoded, frameSamples, marker)
	if m.debeEnabled {
		pkt.Header.Extension = true
		pkt.Header.ExtensionProfile = 0xbede
		pkt.Header.ExtensionData = nil
	}
	m.firstPacketSent = true
	m.rtpPacketsSent++
	m.rtpOctetsSent += uint32(len(pkt.Payload))
	m.lastRtpTs = pkt.Header.Timestamp
	protected, err := m.srtp.Protect(pkt)
	if err != nil {
		m.log.Debug("srtp protect error", "err", err)
		return err
	}
	m.relay.Broadcast(protected)
	return nil
}

func (m *CallManager) notePeerMedia(ssrc uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ssrc == m.selfSsrc {
		return
	}
	m.notePeerMediaLocked()
}

func (m *CallManager) notePeerMediaLocked() {
	m.lastMediaRecv.Store(time.Now().UnixMilli())
	if m.currentCall != nil && m.currentCall.StateData.State == core.CallStateReconnecting {
		if err := m.currentCall.ApplyTransition(Transition{Type: TransitionMediaRestored}); err == nil {
			m.emitState()
			m.log.Info("media path restored", "call_id", m.currentCall.CallID)
		}
	}
}

func (m *CallManager) onRelayData(data []byte) {
	if transport.IsStunPacket(data) {
		return
	}
	if transport.IsRtcpPacket(data) {
		senderSsrc, _ := media.ParseRTCPSenderSSRC(data)
		m.notePeerMedia(senderSsrc)

		m.mu.Lock()
		recvSrtcp := m.recvSrtcp
		recvStats := m.recvStats
		selfSsrc := m.selfSsrc
		obs := m.observer
		callID := ""
		if m.currentCall != nil {
			callID = m.currentCall.CallID
		}
		m.mu.Unlock()
		if recvSrtcp == nil || recvStats == nil {
			return
		}
		plain, err := recvSrtcp.Unprotect(data)
		if err != nil {
			// SRTCP decode failures are control-plane telemetry drops, kept apart from the
			// media-plane SrtpRecvDrop counter/tally so an RTCP fault cannot mask or conflate a
			// genuine audio-decode fault under the same reason label.
			reason := "other"
			var se *media.SrtpError
			if errors.As(err, &se) {
				reason = string(se.Type)
			}
			if m.srtcpDrops.add(reason) {
				m.log.Warn("srtcp recv packet dropped", "reason", reason, "err", err)
			}
			return
		}
		now := uint64(time.Now().UnixMilli())
		in := media.ParseRTCPCompound(plain)
		if in.HasSR {
			recvStats.NoteSenderReport(in.SRNtpMid, now)
		}
		for _, b := range in.Blocks {
			if b.SSRC == selfSsrc && b.LSR != 0 {
				recvStats.NotePeerReportBlock(b.LSR, b.DLSR, b.FractionLost, now)
			}
		}
		q := recvStats.QualitySnapshot(now)
		obs.NoteQuality(q)
		if m.OnQuality != nil && callID != "" {
			m.OnQuality(callID, q)
		}
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
	m.notePeerMediaLocked()
	if pt == core.PayloadTypeWhatsAppOpus && !m.actualPeerSet {
		m.actualPeerSet = true
		if !containsSsrc(m.peerSsrcs, ssrc) {
			m.peerSsrcs = []uint32{ssrc}
			m.relay.SetSubscriptionSsrc(ssrc)
			go m.relay.ResendSubscriptions()
		}
	}
	srtp := m.srtp
	obs := m.observer
	recvStats := m.recvStats
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
		reason := "other"
		var se *media.SrtpError
		if errors.As(err, &se) {
			reason = string(se.Type)
		}
		obs.SrtpRecvDrop(reason)
		if m.srtpDrops.add(reason) {
			m.log.Warn("srtp recv packet dropped", "reason", reason, "err", err)
		} else {
			m.log.Debug("srtp recv packet dropped", "reason", reason, "err", err)
		}
		return
	}
	if len(pkt.Payload) == 0 {
		return
	}
	if recvStats != nil {
		recvStats.NoteRTP(pkt.Header.SequenceNumber, pkt.Header.Timestamp, uint64(time.Now().UnixMilli()))
	}
	handler(pkt)
}

type srtpDropTally struct {
	mu     sync.Mutex
	counts map[string]int64
}

func (t *srtpDropTally) add(reason string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.counts == nil {
		t.counts = map[string]int64{}
	}
	t.counts[reason]++
	return t.counts[reason] == 1
}

func (t *srtpDropTally) snapshotAndReset() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.counts
	t.counts = nil
	return out
}
