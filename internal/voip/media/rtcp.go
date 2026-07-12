package media

import "encoding/binary"

const (
	RTCPPayloadTypeSR       = 200
	RTCPPayloadTypeCompact  = 208
	RTCPPayloadTypeCompact2 = 209
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

func ParseRTCPSenderSSRC(data []byte) (uint32, bool) {
	if len(data) < 8 || (data[0]>>6)&0x03 != 2 {
		return 0, false
	}
	return binary.BigEndian.Uint32(data[4:8]), true
}
