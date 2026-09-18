package call

import (
	"time"

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
	m.videoTxHist = newVideoTxHistory()
	m.videoTxKeyframeRequired = true
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
			m.videoTxHist = newVideoTxHistory() // SSRC/seq novos: histórico antigo não serve
			m.videoTxKeyframeRequired = true
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
	idr := auHasIDR(f.Data)
	if m.videoTxKeyframeRequired {
		// Trava de saída: até o navegador mandar um quadro com IDR de verdade
		// (SPS/PPS/IDR nos NALs, não só a flag f.Keyframe — o navegador às
		// vezes marca o primeiro quadro como keyframe sem ele realmente ter
		// SPS/PPS), descarta tudo. Mandar quadros delta antes de qualquer IDR
		// não dá ao decodificador do WhatsApp nenhum ponto de partida.
		if !idr {
			return
		}
		m.videoTxKeyframeRequired = false
	}
	ts := m.videoTxNextTs
	m.videoTxNextTs += core.WAVideoClockRate / core.WAVideoFrameRate
	m.lastVideoTxTS = ts
	m.lastVideoTxWall = time.Now()
	pkts := m.h264Pay.Packetize(f.Data, ts, idr)
	m.countVideoTxLocked(pkts)
	if VideoDump {
		m.dumpOutboundPackets(pkts, f.Data, f.Keyframe)
		// dumpOutboundPackets só loga os 6 primeiros AUs da chamada; isso aqui
		// não tem esse limite, pra ver se um keyframe de verdade (com SPS/PPS)
		// aparece depois disso — suspeita: o navegador pode nunca estar
		// produzindo SPS/PPS/IDR de verdade mesmo quando f.Keyframe=true.
		if f.Keyframe {
			sps, pps := spsPpsHex(f.Data)
			m.log.Info("VDUMP tx-keyframe-real", "bytes", len(f.Data), "nals", describeAnnexB(f.Data), "sps_hex", sps, "pps_hex", pps)
		}
	}
	for _, pkt := range pkts {
		enc, err := m.srtpSession.Protect(pkt)
		if err != nil {
			m.log.Debug("erro ao proteger vídeo com SRTP", "err", err)
			continue
		}
		if m.videoTxHist != nil {
			m.videoTxHist.put(pkt.Header.SequenceNumber, enc)
		}
		m.videoTxLastSeq = pkt.Header.SequenceNumber
		m.relay.Broadcast(enc)
		if f.Keyframe {
			// Redundância proativa: não conseguimos decifrar o NACK/PLI do
			// WhatsApp pra retransmitir sob pedido (ver VDUMP — o feedback
			// "fast" usa uma chave que não é a peer-a-peer que temos), então
			// mandamos cada pacote do keyframe duas vezes. Duplicata de
			// mesmo seq/SRTP é inofensiva: o receptor descarta a segunda
			// cópia como já vista.
			m.relay.Broadcast(enc)
		}
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
	m.mu.Lock()
	m.countVideoRxLocked(pkt.Header.SequenceNumber)
	m.mu.Unlock()
	frame, keyframe, ok := depay.Push(pkt)
	if !ok {
		return
	}
	if VideoDump {
		m.dumpInboundAccessUnit(frame, keyframe, pkt.Header.Timestamp)
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
	m.stopVideoRtcpLocked()
	m.selfVideoSsrc = 0
	m.peerVideoSsrc = 0
	m.h264Pay = nil
	m.h264Depay = nil
	m.videoTxHist = nil
	m.srtcpSend = nil
	m.srtcpRecv = nil
	m.videoTxPkts = 0
	m.videoTxOctets = 0
	m.videoRxPkts = 0
	m.videoRxHighSeq = 0
	m.videoRtxResent = 0
}
