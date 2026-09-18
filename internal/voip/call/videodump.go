package call

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"wacalls/internal/voip/media"
)

// VideoDump liga o dump detalhado do RTP/RTCP de vídeo (entrada e saída) para
// comparar a nossa packetização com a de um cliente WhatsApp real. Ligado pela
// flag -video-dump do servidor. NÃO deixar ligado em uso normal (verboso).
var VideoDump bool

var (
	vdRxRtp    atomic.Uint64
	vdRxAU     atomic.Uint64
	vdTxRtp    atomic.Uint64
	vdRtcpSeen [8]atomic.Bool // por FMT, pra logar cada tipo uma vez
)

// dumpInboundRelayPacket loga o cabeçalho de todo pacote que chega no relay:
// RTP (cabeçalho + extensão) ou RTCP (tipo + feedback). Chamado de onRelayData.
func (m *CallManager) dumpInboundRelayPacket(data []byte) {
	if !VideoDump || len(data) < 4 {
		return
	}
	b1 := data[1]

	// RTCP: segundo byte é o packet type 200..206.
	if b1 >= 200 && b1 <= 206 {
		fmtv := data[0] & 0x1f
		idx := int(b1 - 200)
		if idx >= 0 && idx < len(vdRtcpSeen) && vdRtcpSeen[idx].CompareAndSwap(false, true) {
			var ssrc uint32
			if len(data) >= 8 {
				ssrc = binary.BigEndian.Uint32(data[4:8])
			}
			m.log.Info("VDUMP rtcp", "pt", b1, "nome", rtcpName(b1, fmtv), "fmt", fmtv,
				"sender_ssrc", ssrc, "bytes", len(data), "hex16", hexN(data, 16))
		}
		return
	}

	pt := b1 & 0x7f
	if pt != 96 && pt != 97 { // só nos interessa vídeo aqui
		return
	}
	h, err := media.DecodeRtpHeader(data)
	if err != nil {
		return
	}
	n := vdRxRtp.Add(1)
	if n <= 60 || n%25 == 0 {
		var payHead string
		hl := h.Size()
		if len(data) > hl {
			payHead = hexN(data[hl:], 4)
		}
		m.log.Info("VDUMP rx-rtp",
			"n", n, "pt", h.PayloadType, "seq", h.SequenceNumber, "ts", h.Timestamp, "ssrc", h.Ssrc,
			"marker", h.Marker, "ext", h.Extension, "ext_profile", fmt.Sprintf("%#x", h.ExtensionProfile),
			"ext_data", fmt.Sprintf("%x", h.ExtensionData), "ext_elems", describeRtpExt(h.ExtensionProfile, h.ExtensionData),
			"hdr_len", hl, "pay_head", payHead)
	}
}

// dumpInboundAccessUnit loga a estrutura NAL de cada AU H264 remontada da
// entrada. Chamado de deliverPeerVideo quando uma AU fica pronta.
func (m *CallManager) dumpInboundAccessUnit(frame []byte, keyframe bool, ts uint32) {
	if !VideoDump {
		return
	}
	n := vdRxAU.Add(1)
	nals := describeAnnexB(frame)
	if keyframe {
		sps, pps := spsPpsHex(frame)
		m.log.Info("VDUMP rx-au", "n", n, "keyframe", keyframe, "ts", ts, "total_bytes", len(frame), "nals", nals, "sps_hex", sps, "pps_hex", pps)
		return
	}
	m.log.Info("VDUMP rx-au", "n", n, "keyframe", keyframe, "ts", ts, "total_bytes", len(frame), "nals", nals)
}

// dumpOutboundPackets loga os primeiros pacotes RTP que o nosso H264Payloader
// gera, pra comparar com rx-rtp. Chamado de FeedCapturedVideo.
func (m *CallManager) dumpOutboundPackets(pkts []*media.RtpPacket, srcFrame []byte, keyframe bool) {
	if !VideoDump {
		return
	}
	n := vdTxRtp.Add(1)
	if n > 6 {
		return
	}
	m.log.Info("VDUMP tx-au", "n", n, "keyframe", keyframe, "src_bytes", len(srcFrame),
		"src_nals", describeAnnexB(srcFrame), "rtp_pkts", len(pkts))
	for i, p := range pkts {
		var head string
		if len(p.Payload) > 0 {
			head = hexN(p.Payload, 4)
		}
		m.log.Info("VDUMP tx-rtp", "au", n, "i", i, "seq", p.Header.SequenceNumber, "ts", p.Header.Timestamp,
			"marker", p.Header.Marker, "ext", p.Header.Extension, "ext_profile", fmt.Sprintf("%#x", p.Header.ExtensionProfile),
			"ext_data", fmt.Sprintf("%x", p.Header.ExtensionData),
			"ext_elems", describeRtpExt(p.Header.ExtensionProfile, p.Header.ExtensionData),
			"pay_len", len(p.Payload), "pay_head", head)
	}
}

// debugLogSrtcpKeys loga a chave/salt/authKey derivados pro SRTCP de entrada
// (efêmero, só desta chamada) — permite testar hipóteses de descriptografia do
// formato "fast" do WhatsApp offline, sem precisar de uma ligação nova a cada
// tentativa. Chamada com m.mu travado (initSrtpKeysLocked/reinitSrtpLocked).
func (m *CallManager) debugLogSrtcpKeys() {
	if !VideoDump || m.srtcpRecv == nil {
		return
	}
	key, salt, authKey := m.srtcpRecv.DebugKeyHex()
	m.log.Info("VDUMP srtcp-recv-key", "session_key", key, "session_salt", salt, "auth_key", authKey)
}

// debugTryNackSsrcCandidates redescriptografa o mesmo SRTCP cru de um NACK
// usando cada SSRC candidato pra derivar o IV (em vez do "sender SSRC" que
// vem em claro no pacote, que os NACKs do WhatsApp sempre mostram como "1" —
// suspeito de ser um placeholder do canal de feedback compacto "fast", não o
// SSRC de verdade usado na cifra). Compara os seqs decodificados contra
// lastSeq (o seq de verdade do último pacote de vídeo que mandamos — NÃO
// txPkts: o H264Payloader começa de um seq aleatório, então txPkts sozinho
// não serve de referência e já gerou falso positivo aqui antes).
func (m *CallManager) debugTryNackSsrcCandidates(raw []byte, seqsAtual []uint16, txPkts uint32, lastSeq uint16, recv *media.SrtcpContext, candidatos map[string]uint32) {
	if recv == nil {
		return
	}
	// Candidatos extra além dos SSRCs conhecidos da chamada: zero (alguns
	// protocolos usam 0 pra "sem stream específico"), e as variantes de
	// byte-swap/complemento dos vídeos — cobre confusão de endianness ou
	// um bug de sinal/complemento na ponta do WhatsApp, hipóteses baratas
	// de testar que ainda não tínhamos coberto.
	extra := map[string]uint32{"zero": 0}
	if v, ok := candidatos["self_video"]; ok {
		extra["self_video_byteswap"] = byteSwap32(v)
		extra["self_video_complement"] = ^v
	}
	if v, ok := candidatos["peer_video"]; ok {
		extra["peer_video_byteswap"] = byteSwap32(v)
		extra["peer_video_complement"] = ^v
	}
	for nome, ssrc := range extra {
		candidatos[nome] = ssrc
	}

	for nome, ssrc := range candidatos {
		plain, err := recv.UnprotectWithSSRC(raw, ssrc, true)
		if err != nil {
			m.log.Info("VDUMP nack-ssrc-tentativa", "candidato", nome, "ssrc", ssrc, "erro", err)
			continue
		}
		mediaSSRC, seqs, ok := media.ParseNACK(plain)
		// Cada entrada FCI do NACK (RFC 4585) é um PID de 16 bits + bitmap dos
		// 16 seguintes — então um NACK de 16 bytes tem só 1 PID independente
		// de verdade (o resto de "seqs" é derivado dele, não são valores
		// independentes). Testar "caiu numa janela plausível" nesse caso tem
		// ~3% de chance de bater por acaso; com ~10 candidatos por pacote,
		// falso positivo vira rotina (confirmado ao vivo: candidatos
		// diferentes "batendo" a cada poucos segundos, sem nenhum se repetir
		// de forma consistente). Só marcar como sinal forte quando o pacote
		// tem 2+ entradas FCI (2+ PIDs independentes teriam que bater juntos).
		entries := (len(plain) - 12) / 4
		plausivel := ok && entries >= 2 && nackSeqsPlausible(seqs, lastSeq)
		if plausivel {
			// WARN pra destacar no meio do ruído: esse candidato decodificou
			// 2+ PIDs independentes todos dentro de uma janela recente
			// plausível — evidência bem mais forte que um único PID batendo.
			m.log.Warn("VDUMP nack-ssrc-tentativa CANDIDATO PLAUSÍVEL", "candidato", nome, "ssrc_usado_no_iv", ssrc,
				"media_ssrc_decodificado", mediaSSRC, "seqs", seqs, "fci_entries", entries, "tx_last_seq", lastSeq)
			continue
		}
		m.log.Info("VDUMP nack-ssrc-tentativa", "candidato", nome, "ssrc_usado_no_iv", ssrc,
			"parseou", ok, "media_ssrc_decodificado", mediaSSRC, "seqs", seqs, "fci_entries", entries,
			"tx_pkts_atual", txPkts, "tx_last_seq", lastSeq)
	}
}

func byteSwap32(v uint32) uint32 {
	return (v>>24)&0xff | (v>>8)&0xff00 | (v<<8)&0xff0000 | (v<<24)&0xff000000
}

// nackSeqsPlausible reporta se os seqs decodificados de um NACK caem numa
// janela recente (até 2000 pacotes) atrás do seq de verdade do último
// pacote de vídeo que mandamos — um decode com a chave/SSRC errado produz
// seqs efetivamente aleatórios, então cair nessa janela é sinal forte de
// que a decifração dessa tentativa está correta. Usa aritmética uint16 com
// wraparound (seq é de 16 bits e dá a volta).
func nackSeqsPlausible(seqs []uint16, lastSeq uint16) bool {
	if len(seqs) == 0 {
		return false
	}
	for _, s := range seqs {
		if lastSeq-s > 2000 {
			return false
		}
	}
	return true
}

// describeRtpExt decodifica um bloco de extensão RTP de cabeçalho de um byte
// (RFC 8285, profile 0xBEDE — as capturas do WhatsApp chegam como 0xDEBE) numa
// string tipo "id3=03 id5=152b id6=0030 id9=0cc5", para comparar byte a byte a
// nossa extensão de saída com a de um cliente WhatsApp real recebida pela via de
// entrada (que já funciona).
func describeRtpExt(profile uint16, data []byte) string {
	if profile != 0xBEDE && profile != 0xDEBE {
		return "(perfil não 1-byte)"
	}
	out := ""
	for i := 0; i < len(data); {
		b := data[i]
		if b == 0 { // padding entre elementos
			i++
			continue
		}
		id := b >> 4
		length := int(b&0x0F) + 1
		i++
		if id == 15 || i+length > len(data) {
			break
		}
		if out != "" {
			out += " "
		}
		out += fmt.Sprintf("id%d=%x", id, data[i:i+length])
		i += length
	}
	if out == "" {
		return "(sem elementos)"
	}
	return out
}

// --- helpers -------------------------------------------------------------

func rtcpName(pt, fmtv byte) string {
	switch pt {
	case 200:
		return "SR"
	case 201:
		return "RR"
	case 202:
		return "SDES"
	case 203:
		return "BYE"
	case 204:
		return "APP"
	case 205:
		switch fmtv {
		case 1:
			return "RTPFB/NACK"
		case 15:
			return "RTPFB/TWCC"
		}
		return "RTPFB"
	case 206:
		switch fmtv {
		case 1:
			return "PSFB/PLI"
		case 2:
			return "PSFB/SLI"
		case 3:
			return "PSFB/RPSI"
		case 4:
			return "PSFB/FIR"
		case 15:
			return "PSFB/AFB(REMB)"
		}
		return "PSFB"
	}
	return "?"
}

func hexN(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return fmt.Sprintf("%x", b[:n])
}

// spsPpsHex devolve o hex do SPS e do PPS (tipos 7/8) de uma unidade de
// acesso Annex-B, pra comparar byte a byte o profile_idc/level_idc do que a
// gente manda (TX) contra o que o celular manda de verdade (RX, que já
// funciona) — nunca comparamos isso diretamente.
func spsPpsHex(frame []byte) (sps, pps string) {
	for _, nal := range media.SplitAnnexBForDump(frame) {
		if len(nal) == 0 {
			continue
		}
		switch nal[0] & 0x1f {
		case 7:
			sps = fmt.Sprintf("%x", nal)
		case 8:
			pps = fmt.Sprintf("%x", nal)
		}
	}
	return sps, pps
}

// auHasIDR reporta se a unidade de acesso Annex-B tem um NAL IDR (tipo 5) —
// gate de FeedCapturedVideo pra nunca mandar quadro delta antes do primeiro
// IDR de verdade (ver videoTxKeyframeRequired). Não exige lock.
func auHasIDR(frame []byte) bool {
	for _, nal := range media.SplitAnnexBForDump(frame) {
		if len(nal) > 0 && nal[0]&0x1f == 5 {
			return true
		}
	}
	return false
}

// describeAnnexB devolve uma string tipo "SPS(12) PPS(4) IDR(20345)" para um
// bitstream Annex-B (usa o splitAnnexB do rtph264.go via media).
func describeAnnexB(frame []byte) string {
	nals := media.SplitAnnexBForDump(frame)
	out := ""
	for _, nal := range nals {
		if len(nal) == 0 {
			continue
		}
		t := nal[0] & 0x1f
		if out != "" {
			out += " "
		}
		out += fmt.Sprintf("%s(%d)", nalName(t), len(nal))
	}
	if out == "" {
		return "(nenhum NAL)"
	}
	return out
}

func nalName(t byte) string {
	switch t {
	case 1:
		return "nonIDR"
	case 5:
		return "IDR"
	case 6:
		return "SEI"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 9:
		return "AUD"
	case 24:
		return "STAP-A"
	case 28:
		return "FU-A"
	}
	return fmt.Sprintf("t%d", t)
}
