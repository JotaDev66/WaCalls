// H264 RTP payload format (RFC 6184) for the video media plane.
//
// WhatsApp video calling negotiates H264 (a real client's <offer> advertises
// enc="h.264" dec="H264,H265,AV1"). The browser encodes with WebCodecs in
// Annex-B form (start-code-delimited NALUs, SPS/PPS in-band on keyframes); this
// file splits an access unit into NALUs and emits them as single-NAL or FU-A
// RTP packets, and reassembles the reverse direction back into an Annex-B frame.
package media

import (
	"wacalls/internal/voip/core"
)

// MaxH264PayloadSize bounds the NAL bytes per RTP packet, matching the VP8 leg:
// the relay data channel caps messages near 1500 bytes and the packet also
// carries the RTP header and the SRTP auth tag.
const MaxH264PayloadSize = 1100

const (
	h264NalTypeMask = 0x1F
	h264FuAType     = 28
	h264StapAType   = 24
)

var annexBStartCode = []byte{0x00, 0x00, 0x00, 0x01}

// cvoExtNoRotation é o bloco de extensão RTP de um byte (profile 0xDEBE) com só
// o elemento CVO (id 3) = 0 (câmera na vertical). Um cliente WhatsApp real anexa
// esse bloco a todo pacote de vídeo; sem ele o receptor pode ignorar o stream.
var cvoExtNoRotation = []byte{0x30, 0x00, 0x00, 0x00}

// H264Payloader splits encoded H264 access units into RTP packets for one
// outbound video stream. Not safe for concurrent use; the call owns one.
type H264Payloader struct {
	ssrc uint32
	seq  uint16
}

func NewH264Payloader(ssrc uint32) *H264Payloader {
	return &H264Payloader{ssrc: ssrc, seq: uint16(randUint(1 << 16))}
}

// Packetize returns the RTP packets for one Annex-B access unit. Every packet
// shares ts (the 90 kHz RTP timestamp); the marker bit is set on the last
// packet of the frame only. An empty or start-code-only frame yields nothing.
func (p *H264Payloader) Packetize(frame []byte, ts uint32) []*RtpPacket {
	nalus := splitAnnexB(frame)
	if len(nalus) == 0 {
		return nil
	}

	var payloads [][]byte
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		if len(nalu) <= MaxH264PayloadSize {
			payloads = append(payloads, nalu)
			continue
		}
		// FU-A fragmentation (RFC 6184 §5.8).
		fbitNri := nalu[0] & 0xE0
		origType := nalu[0] & h264NalTypeMask
		body := nalu[1:]
		frag := MaxH264PayloadSize - 2
		for off := 0; off < len(body); off += frag {
			end := off + frag
			if end > len(body) {
				end = len(body)
			}
			fu := make([]byte, 2+(end-off))
			fu[0] = fbitNri | h264FuAType
			fuHeader := origType
			if off == 0 {
				fuHeader |= 0x80 // start bit
			}
			if end == len(body) {
				fuHeader |= 0x40 // end bit
			}
			fu[1] = fuHeader
			copy(fu[2:], body[off:end])
			payloads = append(payloads, fu)
		}
	}
	if len(payloads) == 0 {
		return nil
	}

	pkts := make([]*RtpPacket, 0, len(payloads))
	for i, pl := range payloads {
		h := NewRtpHeader(core.PayloadTypeWhatsAppH264, p.seq, ts, p.ssrc)
		h.Marker = i == len(payloads)-1
		h.Extension = true
		h.ExtensionProfile = 0xDEBE
		h.ExtensionData = cvoExtNoRotation
		pkts = append(pkts, &RtpPacket{Header: h, Payload: pl})
		p.seq++
	}
	return pkts
}

// H264Depacketizer reassembles Annex-B access units from inbound RTP packets.
// Like the audio leg it assumes in-order delivery over the ordered relay
// channel; a sequence gap drops the frame in progress.
type H264Depacketizer struct {
	frame    []byte
	started  bool
	keyframe bool

	fuBuf    []byte
	fuActive bool
	fuHeader byte

	lastSeq uint16
	haveSeq bool
}

func NewH264Depacketizer() *H264Depacketizer { return &H264Depacketizer{} }

// Push feeds one RTP packet. It returns (frame, keyframe, true) once a packet
// carrying the marker bit completes an access unit, and (nil, false, false)
// while more packets are expected or a packet had to be discarded. frame is
// Annex-B (each NALU prefixed with 00 00 00 01).
func (d *H264Depacketizer) Push(pkt *RtpPacket) (frame []byte, keyframe bool, complete bool) {
	if pkt == nil || len(pkt.Payload) < 1 {
		d.reset()
		return nil, false, false
	}

	seq := pkt.Header.SequenceNumber
	if d.started && d.haveSeq && seq != d.lastSeq+1 {
		d.reset()
	}
	d.lastSeq = seq
	d.haveSeq = true

	for _, nalu := range d.depacketize(pkt.Payload) {
		d.appendNALU(nalu)
	}

	if pkt.Header.Marker && d.started {
		out := make([]byte, len(d.frame))
		copy(out, d.frame)
		kf := d.keyframe
		d.reset()
		return out, kf, true
	}
	return nil, false, false
}

func (d *H264Depacketizer) appendNALU(nalu []byte) {
	if len(nalu) == 0 {
		return
	}
	d.frame = append(d.frame, annexBStartCode...)
	d.frame = append(d.frame, nalu...)
	d.started = true
	switch nalu[0] & h264NalTypeMask {
	case 5, 7, 8: // IDR slice, SPS, PPS
		d.keyframe = true
	}
}

// depacketize turns one RTP payload into zero or more complete NALUs (single
// NAL, STAP-A aggregation, or the finished result of an FU-A series).
func (d *H264Depacketizer) depacketize(payload []byte) [][]byte {
	naluType := payload[0] & h264NalTypeMask

	switch {
	case naluType >= 1 && naluType <= 23:
		d.fuActive = false
		out := make([]byte, len(payload))
		copy(out, payload)
		return [][]byte{out}

	case naluType == h264StapAType:
		d.fuActive = false
		var out [][]byte
		body := payload[1:]
		for len(body) >= 2 {
			size := int(body[0])<<8 | int(body[1])
			body = body[2:]
			if size <= 0 || size > len(body) {
				break
			}
			nalu := make([]byte, size)
			copy(nalu, body[:size])
			out = append(out, nalu)
			body = body[size:]
		}
		return out

	case naluType == h264FuAType:
		if len(payload) < 2 {
			d.fuActive = false
			return nil
		}
		fuHeader := payload[1]
		start := fuHeader&0x80 != 0
		end := fuHeader&0x40 != 0
		origType := fuHeader & h264NalTypeMask
		fbitNri := payload[0] & 0xE0
		body := payload[2:]

		if start {
			d.fuActive = true
			d.fuBuf = append(d.fuBuf[:0], fbitNri|origType)
			d.fuBuf = append(d.fuBuf, body...)
		} else if d.fuActive {
			if len(d.fuBuf)+len(body) > 4<<20 {
				d.fuActive = false
				d.fuBuf = d.fuBuf[:0]
				return nil
			}
			d.fuBuf = append(d.fuBuf, body...)
		} else {
			return nil
		}

		if end && d.fuActive {
			d.fuActive = false
			out := make([]byte, len(d.fuBuf))
			copy(out, d.fuBuf)
			d.fuBuf = d.fuBuf[:0]
			return [][]byte{out}
		}
		return nil

	default:
		d.fuActive = false
		return nil
	}
}

func (d *H264Depacketizer) reset() {
	d.frame = d.frame[:0]
	d.started = false
	d.keyframe = false
	d.fuActive = false
	d.fuBuf = d.fuBuf[:0]
}

// splitAnnexB slices an Annex-B bitstream into its NALUs (no start codes,
// trailing zero bytes trimmed).
func splitAnnexB(data []byte) [][]byte {
	var nalus [][]byte
	start := -1
	i := 0
	for i < len(data) {
		sc := annexBStartCodeLen(data, i)
		if sc > 0 {
			if start >= 0 {
				end := i
				for end > start && data[end-1] == 0 {
					end--
				}
				if end > start {
					nalus = append(nalus, data[start:end])
				}
			}
			i += sc
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(data) {
		nalus = append(nalus, data[start:])
	}
	return nalus
}

func annexBStartCodeLen(data []byte, off int) int {
	if off+3 < len(data) && data[off] == 0 && data[off+1] == 0 && data[off+2] == 0 && data[off+3] == 1 {
		return 4
	}
	if off+2 < len(data) && data[off] == 0 && data[off+1] == 0 && data[off+2] == 1 {
		return 3
	}
	return 0
}

// CVORotationDegrees lê a rotação da extensão RTP CVO (Coordination of Video
// Orientation, RFC 7742 §4) que o WhatsApp anexa aos pacotes de vídeo. Retorna
// os graus no sentido horário (0/90/180/270) e ok=false quando não há CVO.
//
// A extensão de um byte tem o formato | 0 0 0 0 C F R1 R0 |; a rotação é
// (R1<<1)|R0. O ID do elemento não é negociado (não há SDP), então qualquer
// elemento de 1 byte de dados cujo valor caiba em 4 bits (0x00..0x0F) é tratado
// como CVO.
func CVORotationDegrees(h *RtpHeader) (deg uint16, ok bool) {
	// WhatsApp usa o formato de um byte (RFC 8285). O "magic" costuma ser
	// 0xBEDE, mas as capturas do WhatsApp chegam como 0xDEBE — aceitamos os dois.
	if h == nil || !h.Extension || (h.ExtensionProfile != 0xBEDE && h.ExtensionProfile != 0xDEBE) {
		return 0, false
	}
	data := h.ExtensionData
	i := 0
	for i < len(data) {
		b := data[i]
		if b == 0 { // padding
			i++
			continue
		}
		id := b >> 4
		length := int(b&0x0F) + 1
		i++
		if id == 15 || i+length > len(data) {
			break
		}
		if length == 1 && data[i]&0xF0 == 0 {
			switch data[i] & 0x03 {
			case 1:
				return 90, true
			case 2:
				return 180, true
			case 3:
				return 270, true
			default:
				return 0, true
			}
		}
		i += length
	}
	return 0, false
}
