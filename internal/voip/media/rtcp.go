package media

import (
	"encoding/binary"
	"time"
)

// Payload types RTCP usados aqui (RFC 3550 / 4585 / draft-alvestrand-rmcat-remb).
const (
	RTCPTypeSR    = 200
	RTCPTypeRR    = 201
	RTCPTypeSDES  = 202
	RTCPTypeBYE   = 203
	RTCPTypeRTPFB = 205 // NACK / transport-cc
	RTCPTypePSFB  = 206 // PLI / FIR / REMB
)

// ntpNow devolve o timestamp NTP de 64 bits (segundos desde 1900 << 32 | frac).
func ntpNow() uint64 {
	const ntpEpochOffset = 2208988800 // segundos entre 1900 e 1970
	now := time.Now()
	sec := uint64(now.Unix()) + ntpEpochOffset
	frac := uint64(now.Nanosecond()) << 32 / 1e9
	return sec<<32 | frac
}

// BuildSenderReport monta um SR mínimo (sem report blocks) para o SSRC de envio.
func BuildSenderReport(ssrc uint32, rtpTS uint32, packetCount, octetCount uint32) []byte {
	b := make([]byte, 28)
	b[0] = 0x80 // V=2, P=0, RC=0
	b[1] = RTCPTypeSR
	binary.BigEndian.PutUint16(b[2:], 6) // length em words de 32 bits menos 1
	binary.BigEndian.PutUint32(b[4:], ssrc)
	binary.BigEndian.PutUint64(b[8:], ntpNow())
	binary.BigEndian.PutUint32(b[16:], rtpTS)
	binary.BigEndian.PutUint32(b[20:], packetCount)
	binary.BigEndian.PutUint32(b[24:], octetCount)
	return b
}

// BuildReceiverReport monta um RR com um report block sobre o stream recebido de
// remoteSSRC. lastSR/dlsr = os campos LSR/DLSR de RFC 3550 (0 se não sabemos).
func BuildReceiverReport(ssrc, remoteSSRC uint32, fractionLost byte, cumulativeLost uint32,
	extHighestSeq uint32, jitter uint32, lastSR uint32, dlsr uint32) []byte {
	b := make([]byte, 32)
	b[0] = 0x81 // V=2, P=0, RC=1
	b[1] = RTCPTypeRR
	binary.BigEndian.PutUint16(b[2:], 7)
	binary.BigEndian.PutUint32(b[4:], ssrc)
	// report block
	binary.BigEndian.PutUint32(b[8:], remoteSSRC)
	b[12] = fractionLost
	b[13] = byte(cumulativeLost >> 16)
	b[14] = byte(cumulativeLost >> 8)
	b[15] = byte(cumulativeLost)
	binary.BigEndian.PutUint32(b[16:], extHighestSeq)
	binary.BigEndian.PutUint32(b[20:], jitter)
	binary.BigEndian.PutUint32(b[24:], lastSR)
	binary.BigEndian.PutUint32(b[28:], dlsr)
	return b
}

// BuildREMB monta um PSFB/AFB REMB (goog-remb) anunciando bitrateBps como a banda
// que conseguimos receber de mediaSSRC.
func BuildREMB(senderSSRC, mediaSSRC uint32, bitrateBps uint64) []byte {
	// exp/mantissa: mantissa em 18 bits, exp em 6 bits.
	var exp uint32
	m := bitrateBps
	for m >= (1 << 18) {
		m >>= 1
		exp++
	}
	b := make([]byte, 24)
	b[0] = 0x8f // V=2, P=0, FMT=15 (AFB)
	b[1] = RTCPTypePSFB
	binary.BigEndian.PutUint16(b[2:], 5)
	binary.BigEndian.PutUint32(b[4:], senderSSRC)
	binary.BigEndian.PutUint32(b[8:], 0) // media SSRC = 0 para AFB
	copy(b[12:16], []byte("REMB"))
	b[16] = 1 // num SSRCs
	b[17] = byte(exp<<2) | byte(m>>16)
	b[18] = byte(m >> 8)
	b[19] = byte(m)
	binary.BigEndian.PutUint32(b[20:], mediaSSRC)
	return b
}

// RTCPPacketInfo é um resumo de um pacote dentro de um compound RTCP.
type RTCPPacketInfo struct {
	Type      byte
	FMT       byte   // RC/FMT (5 bits baixos do primeiro byte)
	SSRC      uint32 // sender SSRC
	MediaSSRC uint32 // para PSFB/RTPFB
	Name      string
}

// ParseRTCPCompound percorre um compound RTCP em claro e resume cada sub-pacote.
func ParseRTCPCompound(data []byte) []RTCPPacketInfo {
	var out []RTCPPacketInfo
	for len(data) >= 4 {
		rc := data[0] & 0x1f
		pt := data[1]
		length := int(binary.BigEndian.Uint16(data[2:])) // words - 1
		total := (length + 1) * 4
		if total < 4 || total > len(data) {
			break
		}
		info := RTCPPacketInfo{Type: pt, FMT: rc, Name: rtcpTypeName(pt, rc)}
		if len(data) >= 8 {
			info.SSRC = binary.BigEndian.Uint32(data[4:8])
		}
		if (pt == RTCPTypePSFB || pt == RTCPTypeRTPFB) && len(data) >= 12 {
			info.MediaSSRC = binary.BigEndian.Uint32(data[8:12])
		}
		out = append(out, info)
		data = data[total:]
	}
	return out
}

// ParseREMB extrai o bitrate (bps) e os SSRCs alvo de um pacote PSFB/AFB REMB
// (o pacote inteiro, começando no byte de versão). ok=false se não for REMB.
func ParseREMB(pkt []byte) (bitrate uint64, ssrcs []uint32, ok bool) {
	if len(pkt) < 20 || pkt[1] != RTCPTypePSFB || (pkt[0]&0x1f) != 15 {
		return 0, nil, false
	}
	if string(pkt[12:16]) != "REMB" {
		return 0, nil, false
	}
	num := int(pkt[16])
	exp := uint64(pkt[17] >> 2)
	mant := (uint64(pkt[17]&0x03) << 16) | (uint64(pkt[18]) << 8) | uint64(pkt[19])
	bitrate = mant << exp
	off := 20
	for i := 0; i < num && off+4 <= len(pkt); i++ {
		ssrcs = append(ssrcs, binary.BigEndian.Uint32(pkt[off:]))
		off += 4
	}
	return bitrate, ssrcs, true
}

// splitCompound devolve os sub-pacotes RTCP crus de um compound em claro.
func splitCompound(data []byte) [][]byte {
	var out [][]byte
	for len(data) >= 4 {
		total := (int(binary.BigEndian.Uint16(data[2:])) + 1) * 4
		if total < 4 || total > len(data) {
			break
		}
		out = append(out, data[:total])
		data = data[total:]
	}
	return out
}

// SplitRTCPCompound expõe splitCompound.
func SplitRTCPCompound(data []byte) [][]byte { return splitCompound(data) }

// RTCPName expõe o nome legível de um tipo/FMT RTCP.
func RTCPName(pt, rc byte) string { return rtcpTypeName(pt, rc) }

func rtcpTypeName(pt, rc byte) string {
	switch pt {
	case RTCPTypeSR:
		return "SR"
	case RTCPTypeRR:
		return "RR"
	case RTCPTypeSDES:
		return "SDES"
	case RTCPTypeBYE:
		return "BYE"
	case RTCPTypeRTPFB:
		switch rc {
		case 1:
			return "RTPFB/NACK"
		case 15:
			return "RTPFB/TWCC"
		}
		return "RTPFB"
	case RTCPTypePSFB:
		switch rc {
		case 1:
			return "PSFB/PLI"
		case 4:
			return "PSFB/FIR"
		case 15:
			return "PSFB/AFB(REMB)"
		}
		return "PSFB"
	}
	return "?"
}
