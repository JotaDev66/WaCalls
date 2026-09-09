package call

import (
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

// initVideoLocked prepara os SSRCs de vídeo, o payloader e o depacketizer de uma
// chamada de vídeo e informa ao relay qual SSRC de vídeo enviar / assinar.
// Chamada com m.mu travado. selfJid/peerJid são os melhores JIDs conhecidos no
// momento; são refinados para device JIDs depois via refineVideoSsrcLocked,
// espelhando o áudio.
func (m *CallManager) initVideoLocked(callID, selfJid, peerJid string) {
	m.selfVideoSsrc = media.GenerateSecureSsrc(callID, selfJid, core.VideoSsrcCounter)
	m.peerVideoSsrc = media.GenerateSecureSsrc(callID, peerJid, core.VideoSsrcCounter)
	m.h264Pay = media.NewH264Payloader(m.selfVideoSsrc)
	m.h264Depay = media.NewH264Depacketizer()
	m.relay.SetVideoSsrc(m.selfVideoSsrc)
	m.relay.SetVideoSubscriptionSsrc(m.peerVideoSsrc)
}

// refineVideoSsrcLocked recalcula os SSRCs de vídeo a partir dos device JIDs
// assim que a lista de participantes do ack de relay é conhecida, do mesmo jeito
// que o áudio faz. Chamada com m.mu travado. Um jid vazio deixa aquele lado
// intocado.
func (m *CallManager) refineVideoSsrcLocked(callID, selfDeviceJid, peerDeviceJid string) {
	if m.h264Pay == nil {
		return
	}
	if selfDeviceJid != "" {
		if s := media.GenerateSecureSsrc(callID, ensureDeviceJid(selfDeviceJid), core.VideoSsrcCounter); s != m.selfVideoSsrc {
			m.selfVideoSsrc = s
			m.h264Pay = media.NewH264Payloader(s)
			m.relay.SetVideoSsrc(s)
		}
	}
	if peerDeviceJid != "" {
		if s := media.GenerateSecureSsrc(callID, ensureDeviceJid(peerDeviceJid), core.VideoSsrcCounter); s != m.peerVideoSsrc {
			m.peerVideoSsrc = s
			m.relay.SetVideoSubscriptionSsrc(s)
		}
	}
}

// FeedCapturedVideo pega uma unidade de acesso H264 (Annex-B) codificada vinda do
// browser, empacota em RTP protegido por SRTP e transmite para os relays
// conectados. É no-op enquanto o plano de vídeo e a conexão de relay não estão
// prontos.
//
// Todo o empacota/protege/transmite roda sob m.mu: o contexto de envio SRTP é
// compartilhado com a perna de áudio (sendOpusFrameLocked também segura m.mu), e
// áudio e vídeo chegam em data channels separados do browser, então sem o lock
// os dois competiriam pelo estado de sequência/ROC do SRTP.
func (m *CallManager) FeedCapturedVideo(f media.VideoFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.h264Pay == nil || m.srtpSession == nil || !m.relay.HasConnection() || len(f.Data) == 0 {
		return
	}
	ts := media.VideoTimestamp(f.TimestampMS)
	for _, pkt := range m.h264Pay.Packetize(f.Data, ts) {
		enc, err := m.srtpSession.Protect(pkt)
		if err != nil {
			m.log.Debug("erro ao proteger vídeo com SRTP", "err", err)
			continue
		}
		m.relay.Broadcast(enc)
	}
}

// deliverPeerVideo decifra um pacote RTP de vídeo de entrada e, quando ele
// completa uma unidade de acesso, entrega para OnPeerVideo. Chamada sem m.mu
// travado (srtp e depay são capturados sob o lock pelo chamador), como o
// caminho do áudio.
func (m *CallManager) deliverPeerVideo(srtp *media.SrtpSession, depay *media.H264Depacketizer, data []byte) {
	if srtp == nil || depay == nil {
		return
	}
	pkt, err := srtp.Unprotect(data)
	if err != nil {
		m.log.Debug("erro ao decifrar vídeo com SRTP", "err", err)
		return
	}
	rot, _ := media.CVORotationDegrees(pkt.Header)
	frame, keyframe, ok := depay.Push(pkt)
	if !ok {
		return
	}
	if m.OnPeerVideo != nil {
		m.OnPeerVideo(media.VideoFrame{
			Keyframe:    keyframe,
			TimestampMS: uint32(uint64(pkt.Header.Timestamp) * 1000 / core.WAVideoClockRate),
			Rotation:    rot,
			Data:        frame,
		})
	}
}

// cleanupVideoLocked descarta o plano de vídeo. Chamada com m.mu travado a
// partir de cleanupMedia.
func (m *CallManager) cleanupVideoLocked() {
	m.selfVideoSsrc = 0
	m.peerVideoSsrc = 0
	m.h264Pay = nil
	m.h264Depay = nil
}
