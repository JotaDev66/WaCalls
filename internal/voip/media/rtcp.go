package media

import "encoding/binary"

const (
	RTCPPayloadTypeSR       = 200
	RTCPPayloadTypeRR       = 201
	RTCPPayloadTypeSDES     = 202
	RTCPPayloadTypeCompact  = 208
	RTCPPayloadTypeCompact2 = 209

	sdesItemCNAME = 0x01
)

const ntpUnixOffsetSecs = 2208988800

type RTCPSenderStats struct {
	PacketsSent  uint32
	OctetsSent   uint32
	RtpTimestamp uint32
}

func BuildCompact208(local, remote uint32) [12]byte {
	var buf [12]byte
	buf[0] = 0x81
	buf[1] = RTCPPayloadTypeCompact
	buf[3] = 2
	binary.BigEndian.PutUint32(buf[4:8], local)
	binary.BigEndian.PutUint32(buf[8:12], remote)
	return buf
}

func BuildCompact209(local uint32) [8]byte {
	var buf [8]byte
	buf[0] = 0x81
	buf[1] = RTCPPayloadTypeCompact2
	buf[3] = 1
	binary.BigEndian.PutUint32(buf[4:8], local)
	return buf
}

func BuildSenderReport(local uint32, s RTCPSenderStats, nowMs uint64) [28]byte {
	var buf [28]byte
	buf[0] = 0x80
	buf[1] = RTCPPayloadTypeSR
	buf[3] = 6
	binary.BigEndian.PutUint32(buf[4:8], local)
	ntpSec := uint32(nowMs/1000 + ntpUnixOffsetSecs)
	ntpFrac := uint32(float64(nowMs%1000) / 1000.0 * 4294967296.0)
	binary.BigEndian.PutUint32(buf[8:12], ntpSec)
	binary.BigEndian.PutUint32(buf[12:16], ntpFrac)
	binary.BigEndian.PutUint32(buf[16:20], s.RtpTimestamp)
	binary.BigEndian.PutUint32(buf[20:24], s.PacketsSent)
	binary.BigEndian.PutUint32(buf[24:28], s.OctetsSent)
	return buf
}

// RTCPReportBlock is one receiver report block (RFC 3550 6.4.1): reception stats about a peer SSRC.
type RTCPReportBlock struct {
	SSRC           uint32
	FractionLost   uint8
	CumulativeLost uint32 // 24-bit
	ExtHighSeq     uint32
	Jitter         uint32
	LSR            uint32
	DLSR           uint32
}

// BuildSenderReportWithBlock builds a Sender Report; when rb != nil it carries one report block (RC=1).
func BuildSenderReportWithBlock(local uint32, s RTCPSenderStats, rb *RTCPReportBlock, nowMs uint64) []byte {
	size := 28
	rc := byte(0)
	if rb != nil {
		size = 52
		rc = 1
	}
	buf := make([]byte, size)
	buf[0] = 0x80 | rc
	buf[1] = RTCPPayloadTypeSR
	binary.BigEndian.PutUint16(buf[2:4], uint16(size/4-1))
	binary.BigEndian.PutUint32(buf[4:8], local)
	ntpSec := uint32(nowMs/1000 + ntpUnixOffsetSecs)
	ntpFrac := uint32(float64(nowMs%1000) / 1000.0 * 4294967296.0)
	binary.BigEndian.PutUint32(buf[8:12], ntpSec)
	binary.BigEndian.PutUint32(buf[12:16], ntpFrac)
	binary.BigEndian.PutUint32(buf[16:20], s.RtpTimestamp)
	binary.BigEndian.PutUint32(buf[20:24], s.PacketsSent)
	binary.BigEndian.PutUint32(buf[24:28], s.OctetsSent)
	if rb != nil {
		binary.BigEndian.PutUint32(buf[28:32], rb.SSRC)
		buf[32] = rb.FractionLost
		buf[33] = byte(rb.CumulativeLost >> 16)
		buf[34] = byte(rb.CumulativeLost >> 8)
		buf[35] = byte(rb.CumulativeLost)
		binary.BigEndian.PutUint32(buf[36:40], rb.ExtHighSeq)
		binary.BigEndian.PutUint32(buf[40:44], rb.Jitter)
		binary.BigEndian.PutUint32(buf[44:48], rb.LSR)
		binary.BigEndian.PutUint32(buf[48:52], rb.DLSR)
	}
	return buf
}

// BuildSDES builds an SDES packet (PT 202) with a single CNAME item for the given SSRC.
func BuildSDES(ssrc uint32, cname string) []byte {
	// chunk = ssrc(4) + item(type,len,cname) + 0x00 terminator, then zero-padded so the whole
	// packet is a 32-bit multiple.
	chunk := make([]byte, 0, 4+2+len(cname)+1)
	var ssrcBuf [4]byte
	binary.BigEndian.PutUint32(ssrcBuf[:], ssrc)
	chunk = append(chunk, ssrcBuf[:]...)
	chunk = append(chunk, sdesItemCNAME, byte(len(cname)))
	chunk = append(chunk, cname...)
	chunk = append(chunk, 0x00)
	if pad := (4 - (4+len(chunk))%4) % 4; pad > 0 {
		chunk = append(chunk, make([]byte, pad)...)
	}
	buf := make([]byte, 4+len(chunk))
	buf[0] = 0x81 // V=2, SC=1
	buf[1] = RTCPPayloadTypeSDES
	binary.BigEndian.PutUint16(buf[2:4], uint16((4+len(chunk))/4-1))
	copy(buf[4:], chunk)
	return buf
}

// BuildRTCPCompound builds a Sender Report (with an optional report block) followed by an SDES
// CNAME, the compound the WhatsApp client sends periodically.
func BuildRTCPCompound(local uint32, s RTCPSenderStats, rb *RTCPReportBlock, cname string, nowMs uint64) []byte {
	return append(BuildSenderReportWithBlock(local, s, rb, nowMs), BuildSDES(local, cname)...)
}

func ParseRTCPSenderSSRC(data []byte) (uint32, bool) {
	if len(data) < 8 || (data[0]>>6)&0x03 != 2 {
		return 0, false
	}
	return binary.BigEndian.Uint32(data[4:8]), true
}

// InboundReportBlock is a receiver report block parsed from an inbound peer SR or RR: the peer's
// reception stats about the SSRC it reports on, including the LSR/DLSR we need to compute RTT.
type InboundReportBlock struct {
	SSRC           uint32
	FractionLost   uint8
	CumulativeLost uint32
	ExtHighSeq     uint32
	Jitter         uint32
	LSR            uint32
	DLSR           uint32
}

// InboundRTCP is the quality-relevant content of a decoded inbound RTCP compound.
type InboundRTCP struct {
	HasSR    bool
	SRNtpMid uint32
	Blocks   []InboundReportBlock
}

// ParseRTCPCompound walks a plaintext RTCP compound and extracts the peer SR NTP mid-32 and any
// receiver report blocks (from SRs with RC>0 or RRs). Malformed or truncated input stops the walk.
func ParseRTCPCompound(data []byte) InboundRTCP {
	var out InboundRTCP
	for off := 0; off+4 <= len(data); {
		if (data[off]>>6)&0x03 != 2 {
			break
		}
		rc := int(data[off] & 0x1f)
		pt := data[off+1]
		pktLen := (int(binary.BigEndian.Uint16(data[off+2:off+4])) + 1) * 4
		if pktLen < 4 || off+pktLen > len(data) {
			break
		}
		switch pt {
		case RTCPPayloadTypeSR:
			if pktLen >= 28 {
				out.HasSR = true
				out.SRNtpMid = binary.BigEndian.Uint32(data[off+10 : off+14])
				parseReportBlocks(data[off+28:off+pktLen], rc, &out.Blocks)
			}
		case RTCPPayloadTypeRR:
			if pktLen >= 8 {
				parseReportBlocks(data[off+8:off+pktLen], rc, &out.Blocks)
			}
		}
		off += pktLen
	}
	return out
}

func parseReportBlocks(b []byte, rc int, blocks *[]InboundReportBlock) {
	for i := 0; i < rc && (i+1)*24 <= len(b); i++ {
		p := b[i*24:]
		*blocks = append(*blocks, InboundReportBlock{
			SSRC:           binary.BigEndian.Uint32(p[0:4]),
			FractionLost:   p[4],
			CumulativeLost: uint32(p[5])<<16 | uint32(p[6])<<8 | uint32(p[7]),
			ExtHighSeq:     binary.BigEndian.Uint32(p[8:12]),
			Jitter:         binary.BigEndian.Uint32(p[12:16]),
			LSR:            binary.BigEndian.Uint32(p[16:20]),
			DLSR:           binary.BigEndian.Uint32(p[20:24]),
		})
	}
}
