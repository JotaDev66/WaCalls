package call

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

func hexBytes(b []byte) string { return hex.EncodeToString(b) }

// videoProactiveKeyframeEveryTicks: a cada quantos ticks do loop de RTCP
// (1/s) pedimos um keyframe novo por conta própria, sem esperar PLI/FIR do
// peer. 2s é um meio-termo entre dar mais chances de um keyframe inteiro
// chegar e não gastar banda demais mandando keyframe toda hora.
const videoProactiveKeyframeEveryTicks = 2

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
	m.videoRtcpTicks++
	requestKeyframeNow := m.videoRtcpTicks%videoProactiveKeyframeEveryTicks == 0
	if requestKeyframeNow {
		m.videoTxKeyframeRequired = true
	}
	kfReq := m.OnKeyframeRequest
	m.mu.Unlock()

	if requestKeyframeNow && kfReq != nil {
		if VideoDump {
			m.log.Info("VDUMP keyframe proativo (periódico, sem depender de PLI/FIR)")
		}
		kfReq()
	}

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
	if err != nil && !errors.Is(err, media.ErrAuthTagMismatch) {
		if VideoDump {
			// header de 8 bytes é sempre claro (RFC 3711) mesmo quando o pacote é
			// curto demais pro nosso authTagLen fixo — loga pra permitir engenharia
			// reversa do formato "fast" do WhatsApp offline (tag/comprimento reais).
			rc := byte(0)
			pt := byte(0)
			if len(data) >= 2 {
				rc = data[0] & 0x1f
				pt = data[1]
			}
			m.log.Info("VDUMP rx-rtcp unprotect erro", "err", err, "bytes", len(data),
				"pt_claro", pt, "nome_claro", media.RTCPName(pt, rc), "rc_fmt_claro", rc,
				"hex_completo", hexBytes(data))
		}
		return
	}
	if err != nil {
		// Tag de auth não bateu, mas processamos mesmo assim: a derivação da
		// chave do canal "fast" do WhatsApp (NACK) ainda não está confirmada,
		// e bloquear aqui quebra a retransmissão de vídeo. Loga sempre (não só
		// VideoDump) porque é sinal relevante até isso ser resolvido de vez.
		rc := byte(0)
		pt := byte(0)
		if len(data) >= 2 {
			rc = data[0] & 0x1f
			pt = data[1]
		}
		m.log.Warn("SRTCP auth tag não bateu — processando mesmo assim (chave do canal fast ainda não confirmada)",
			"pt_claro", pt, "nome_claro", media.RTCPName(pt, rc), "rc_fmt_claro", rc)
	}
	m.mu.Lock()
	selfV := m.selfVideoSsrc
	peerV := m.peerVideoSsrc
	selfA := m.selfSsrc
	peerA := firstSsrc(m.peerSsrcs)
	kfReq := m.OnKeyframeRequest
	txPkts := m.videoTxPkts
	txLastSeq := m.videoTxLastSeq
	recvCtx := m.srtcpRecv
	m.mu.Unlock()

	if VideoDump {
		m.log.Info("VDUMP rx-rtcp bruto/decifrado", "bytes_cifrado", len(data),
			"hex_cifrado", hexBytes(data), "bytes_claro", len(plain), "hex_claro", hexBytes(plain))
	}

	for _, sub := range media.SplitRTCPCompound(plain) {
		if mediaSSRC, seqs, ok := media.ParseNACK(sub); ok {
			if VideoDump {
				m.log.Info("VDUMP rx-rtcp NACK bruto", "bytes", len(sub), "hex", hexBytes(sub))
				var wireSenderSsrc uint32
				if len(data) >= 8 {
					wireSenderSsrc = binary.BigEndian.Uint32(data[4:8])
				}
				m.debugTryNackSsrcCandidates(data, seqs, txPkts, txLastSeq, recvCtx, map[string]uint32{
					"wire_claro": wireSenderSsrc, "self_video": selfV, "peer_video": peerV,
					"self_audio": selfA, "peer_audio": peerA,
				})
			}
			m.handleVideoNack(mediaSSRC, seqs, selfV)
			continue
		}
		if media.IsPLIOrFIR(sub) {
			if VideoDump {
				m.log.Info("VDUMP rx-rtcp PLI/FIR — peer pediu keyframe", "tem_callback", kfReq != nil,
					"bytes", len(sub), "hex", hexBytes(sub))
			}
			m.log.Debug("RTCP PLI/FIR recebido do peer — pedindo keyframe ao browser")
			m.mu.Lock()
			m.videoTxKeyframeRequired = true
			m.mu.Unlock()
			if kfReq != nil {
				kfReq()
			}
			continue
		}
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
	}
}

// handleVideoNack retransmite os pacotes de vídeo que o peer pediu num RTCP NACK,
// relendo-os do histórico já protegido (SRTP) e reenviando pelo relay sem tocar
// no estado do payloader. Chamada sem m.mu travado.
func (m *CallManager) handleVideoNack(mediaSSRC uint32, seqs []uint16, selfV uint32) {
	m.mu.Lock()
	hist := m.videoTxHist
	m.mu.Unlock()
	if hist == nil || len(seqs) == 0 {
		return
	}
	if mediaSSRC != selfV {
		if VideoDump {
			m.log.Info("VDUMP rx-rtcp NACK ignorado (SSRC não é nosso vídeo)",
				"media_ssrc", mediaSSRC, "self_video_ssrc", selfV)
		}
		return
	}

	var resent, missing int
	for _, seq := range seqs {
		pkt, ok := hist.get(seq)
		if !ok {
			missing++
			continue
		}
		m.relay.Broadcast(pkt)
		resent++
	}

	m.mu.Lock()
	m.videoRtxResent += uint32(resent)
	total := m.videoRtxResent
	m.mu.Unlock()

	if VideoDump {
		m.log.Info("VDUMP rx-rtcp NACK", "media_ssrc", mediaSSRC, "self_video_ssrc", selfV,
			"pedidos", len(seqs), "reenviados", resent, "fora_do_buffer", missing, "reenviados_total", total)
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
