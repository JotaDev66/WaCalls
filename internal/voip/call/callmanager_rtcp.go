package call

import (
	"encoding/hex"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

func hexBytes(b []byte) string { return hex.EncodeToString(b) }

// startVideoRtcpLocked liga o loop de RTCP para a chamada de vídeo: a cada ~1s
// manda um compound SRTCP com SR (nosso stream de vídeo) + RR (relatório do
// stream do peer) + REMB (banda que dizemos aceitar). O WhatsApp roda estimativa
// de banda e não sustenta o vídeo sem esse retorno. Chamada com m.mu travado.
func (m *CallManager) startVideoRtcpLocked() {
	if m.rtcpStop != nil || m.h264Pay == nil || m.srtcpSend == nil {
		return
	}
	stop := make(chan struct{})
	m.rtcpStop = stop
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.sendVideoRtcpTick()
			}
		}
	}()
	m.log.Debug("loop de RTCP de vídeo iniciado")
}

func (m *CallManager) stopVideoRtcpLocked() {
	if m.rtcpStop != nil {
		close(m.rtcpStop)
		m.rtcpStop = nil
	}
}

func (m *CallManager) sendVideoRtcpTick() {
	m.mu.Lock()
	if m.srtcpSend == nil || m.h264Pay == nil || m.srtpSession == nil || !m.relay.HasConnection() {
		m.mu.Unlock()
		return
	}
	selfV := m.selfVideoSsrc
	peerV := m.peerVideoSsrc
	txPkts := m.videoTxPkts
	txOctets := m.videoTxOctets
	rxHigh := m.videoRxHighSeq
	rxPkts := m.videoRxPkts
	// RTP timestamp do SR: extrapola do último pacote de vídeo enviado (RFC 3550
	// exige que corresponda ao stream — o WhatsApp usa isso na estimativa de banda).
	rtpTS := m.lastVideoTxTS
	if !m.lastVideoTxWall.IsZero() {
		rtpTS += uint32(time.Since(m.lastVideoTxWall).Milliseconds()) * core.WAVideoClockRate / 1000
	}
	srtcp := m.srtcpSend
	m.mu.Unlock()

	var cumulativeLost uint32
	// pacotes esperados = (highSeq - firstSeq); aproximamos por rxPkts vs highSeq low16.
	_ = rxPkts

	sr := media.BuildSenderReport(selfV, rtpTS, txPkts, txOctets)
	rr := media.BuildReceiverReport(selfV, peerV, 0, cumulativeLost, rxHigh, 0, 0, 0)
	remb := media.BuildREMB(selfV, peerV, 1_500_000)

	compound := make([]byte, 0, len(sr)+len(rr)+len(remb))
	compound = append(compound, sr...)
	compound = append(compound, rr...)
	compound = append(compound, remb...)

	enc, err := srtcp.Protect(compound)
	if err != nil {
		m.log.Debug("srtcp protect falhou", "err", err)
		return
	}
	m.relay.Broadcast(enc)
	if VideoDump {
		m.log.Info("VDUMP tx-rtcp", "bytes", len(enc), "tx_pkts", txPkts, "rx_high", rxHigh)
	}
}

// handleInboundRtcp descifra e processa um SRTCP recebido do relay. Chamada sem
// m.mu travado.
func (m *CallManager) handleInboundRtcp(data []byte) {
	m.mu.Lock()
	recv := m.srtcpRecv
	m.mu.Unlock()
	if recv == nil {
		return
	}
	plain, err := recv.Unprotect(data)
	if err != nil {
		if VideoDump {
			m.log.Info("VDUMP rx-rtcp unprotect erro", "err", err, "bytes", len(data))
		}
		return
	}
	m.mu.Lock()
	selfV := m.selfVideoSsrc
	m.mu.Unlock()

	for _, sub := range media.SplitRTCPCompound(plain) {
		if br, ssrcs, ok := media.ParseREMB(sub); ok {
			forOurVideo := false
			for _, s := range ssrcs {
				if s == selfV {
					forOurVideo = true
				}
			}
			if VideoDump {
				m.log.Info("VDUMP rx-rtcp REMB", "bitrate", br, "ssrcs", ssrcs,
					"para_nosso_video", forOurVideo, "self_video_ssrc", selfV)
			}
			continue
		}
		if VideoDump && len(sub) >= 2 {
			rc := sub[0] & 0x1f
			hexLen := len(sub)
			if hexLen > 40 {
				hexLen = 40
			}
			m.log.Info("VDUMP rx-rtcp", "tipo", media.RTCPName(sub[1], rc), "pt", sub[1], "fmt", rc,
				"bytes", len(sub), "hex", hexBytes(sub[:hexLen]))
		}
		if len(sub) >= 2 && sub[1] == media.RTCPTypePSFB && (sub[0]&0x1f == 1 || sub[0]&0x1f == 4) {
			m.log.Debug("RTCP PLI/FIR recebido do peer")
		}
	}
}

// countVideoTxLocked contabiliza os pacotes/octetos que mandamos, para o SR.
// Chamada com m.mu travado.
func (m *CallManager) countVideoTxLocked(pkts []*media.RtpPacket) {
	for _, p := range pkts {
		m.videoTxPkts++
		m.videoTxOctets += uint32(len(p.Payload))
	}
}

// countVideoRxLocked registra o maior seq recebido, para o RR. Chamada com
// m.mu travado.
func (m *CallManager) countVideoRxLocked(seq uint16) {
	m.videoRxPkts++
	// extended highest seq: mantém o ROC implícito simples (16 bits só).
	if uint32(seq) > m.videoRxHighSeq {
		m.videoRxHighSeq = uint32(seq)
	}
}
