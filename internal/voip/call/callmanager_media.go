package call

import (
	"time"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

func (m *CallManager) initCodec() {
	if m.codec != nil {
		return
	}
	codec, err := media.NewMLowCodec(media.DefaultCodecOptions)
	if err != nil {
		m.log.Warn("MLow codec unavailable — call will run signaling-only (no audio)", "err", err)
		return
	}
	m.codec = codec
}

func (m *CallManager) FeedCapturedPCM(data []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codec == nil || len(data) == 0 {
		return
	}
	m.captureBuf = append(m.captureBuf, data...)
	if maxBuffered := m.codec.FrameSize() * 4; len(m.captureBuf) > maxBuffered {
		m.captureBuf = m.captureBuf[len(m.captureBuf)-maxBuffered:]
	}
}

func (m *CallManager) sendOpusFrameLocked(opus []byte) {
	if m.rtpSession == nil || m.srtp == nil {
		return
	}
	marker := !m.firstPacketSent
	pkt := m.rtpSession.CreatePacketWithDuration(opus, m.codec.FrameSize(), marker)
	if m.debeEnabled {
		pkt.Header.Extension = true
		pkt.Header.ExtensionProfile = 0xbede
		pkt.Header.ExtensionData = nil
	}
	m.firstPacketSent = true

	srtp, err := m.srtp.Protect(pkt)
	if err != nil {
		m.log.Debug("srtp protect error", "err", err)
		return
	}
	m.relay.Broadcast(srtp)
}

func (m *CallManager) startMediaSendLoopLocked() {
	if m.sendLoopStop != nil || m.codec == nil {
		return
	}
	stop := make(chan struct{})
	m.sendLoopStop = stop
	frameSize := m.codec.FrameSize()
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
			m.mu.Lock()
			if m.codec == nil || m.rtpSession == nil || m.srtp == nil || !m.relay.HasConnection() {
				m.mu.Unlock()
				continue
			}
			frame := silence
			if len(m.captureBuf) >= frameSize {
				frame = make([]float32, frameSize)
				copy(frame, m.captureBuf[:frameSize])
				m.captureBuf = m.captureBuf[frameSize:]
			}
			if opus, err := m.codec.Encode(frame); err == nil {
				m.sendOpusFrameLocked(opus)
			}
			m.mu.Unlock()
		}
	}()
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
	switch data[1] & 0x7f {
	case core.PayloadTypeWhatsAppOpus:
		m.handleAudioRelayData(data)
	case core.PayloadTypeWhatsAppH264:
		m.video.HandleRelayData(data)
	}
}

func (m *CallManager) handleAudioRelayData(data []byte) {
	m.mu.Lock()
	if m.srtp == nil || m.codec == nil {
		m.mu.Unlock()
		return
	}
	ssrc := media.RTPSsrc(data)
	if ssrc == m.selfSsrc {
		m.mu.Unlock()
		return
	}
	if !m.actualPeerSet {
		m.actualPeerSet = true
		if !containsSsrc(m.peerSsrcs, ssrc) {
			m.peerSsrcs = []uint32{ssrc}
			m.relay.SetSubscriptionSsrc(ssrc)
			go m.relay.ResendSubscriptions()
		}
	}
	srtp := m.srtp
	codec := m.codec
	m.mu.Unlock()

	pkt, err := srtp.Unprotect(data)
	if err != nil {
		m.log.Debug("srtp unprotect error", "err", err)
		return
	}
	if len(pkt.Payload) == 0 {
		return
	}
	pcm, err := codec.Decode(pkt.Payload)
	if err != nil || len(pcm) == 0 {
		return
	}
	if m.OnPeerAudio != nil {
		m.OnPeerAudio(m.alignPeerAudio(pkt.Header.Timestamp, pcm))
	}
}

func (m *CallManager) alignPeerAudio(ts uint32, pcm []float32) []float32 {
	const maxGapSamples = 8000
	m.mu.Lock()
	defer m.mu.Unlock()
	origLen := uint64(len(pcm))
	if !m.audioTimelineSet {
		m.audioTimelineSet = true
		m.audioBaseTs = ts
		m.audioPlayedSamples = origLen
		return pcm
	}
	target := uint64(ts - m.audioBaseTs)
	gap := int64(target) - int64(m.audioPlayedSamples)
	if gap < 0 || gap > maxGapSamples {
		m.audioBaseTs = ts
		m.audioPlayedSamples = origLen
		return pcm
	}
	if gap > 0 {
		padded := make([]float32, int(gap)+int(origLen))
		copy(padded[int(gap):], pcm)
		pcm = padded
	}
	m.audioPlayedSamples = target + origLen
	return pcm
}
