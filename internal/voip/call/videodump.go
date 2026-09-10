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
	if n <= 20 || n%50 == 0 {
		var payHead string
		hl := h.Size()
		if len(data) > hl {
			payHead = hexN(data[hl:], 4)
		}
		m.log.Info("VDUMP rx-rtp",
			"n", n, "pt", h.PayloadType, "seq", h.SequenceNumber, "ts", h.Timestamp, "ssrc", h.Ssrc,
			"marker", h.Marker, "ext", h.Extension, "ext_profile", fmt.Sprintf("%#x", h.ExtensionProfile),
			"ext_data", fmt.Sprintf("%x", h.ExtensionData), "hdr_len", hl, "pay_head", payHead)
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
			"ext_data", fmt.Sprintf("%x", p.Header.ExtensionData), "pay_len", len(p.Payload), "pay_head", head)
	}
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
