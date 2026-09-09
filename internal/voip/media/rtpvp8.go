// VP8 RTP payload format (RFC 7741) for the video media plane.
//
// The audio path carries one MLow frame per RTP packet, so it never needs
// fragmentation or reassembly. Video frames are far larger than an MTU, so this
// file adds a payloader that splits an encoded VP8 frame across RTP packets and
// a depacketizer that puts them back together.
//
// Only the mandatory 1-byte descriptor is emitted (X=0: no PictureID / temporal
// layers). The depacketizer additionally tolerates the extended descriptor form
// on inbound packets by parsing and skipping its optional fields, since a real
// WhatsApp sender (phase 2) may use them.
package media

import (
	"wacalls/internal/voip/core"
)

// MaxVP8PayloadSize bounds the VP8 bitstream bytes per RTP packet. The relay
// data channel caps messages near 1500 bytes and the packet also carries the
// RTP header, the 1-byte descriptor and the SRTP auth tag.
const MaxVP8PayloadSize = 1100

const (
	// vp8DescStart is the descriptor for the first packet of a frame:
	// X=0 R=0 N=0 S=1 R=0 PID=0.
	vp8DescStart = 0x10
	// vp8DescCont is the descriptor for the remaining packets: S=0.
	vp8DescCont = 0x00
)

// VP8Payloader splits encoded VP8 frames into RTP packets for one outbound
// video stream. Not safe for concurrent use; the call owns a single instance.
type VP8Payloader struct {
	ssrc uint32
	seq  uint16
}

func NewVP8Payloader(ssrc uint32) *VP8Payloader {
	return &VP8Payloader{ssrc: ssrc, seq: uint16(randUint(1 << 16))}
}

// Packetize returns the RTP packets for one encoded VP8 frame. Every packet of
// the frame shares ts (the 90 kHz RTP timestamp); the marker bit is set on the
// last packet only. An empty frame yields no packets.
func (p *VP8Payloader) Packetize(frame []byte, ts uint32) []*RtpPacket {
	if len(frame) == 0 {
		return nil
	}
	var pkts []*RtpPacket
	for off := 0; off < len(frame); {
		end := off + MaxVP8PayloadSize
		if end > len(frame) {
			end = len(frame)
		}
		desc := byte(vp8DescCont)
		if off == 0 {
			desc = vp8DescStart
		}
		payload := make([]byte, 1+(end-off))
		payload[0] = desc
		copy(payload[1:], frame[off:end])

		h := NewRtpHeader(core.PayloadTypeWhatsAppVP8, p.seq, ts, p.ssrc)
		h.Marker = end == len(frame)
		pkts = append(pkts, &RtpPacket{Header: h, Payload: payload})

		p.seq++
		off = end
	}
	return pkts
}

// VP8Depacketizer reassembles encoded VP8 frames from inbound RTP packets. Like
// the audio leg it assumes in-order delivery over the ordered relay channel; a
// sequence gap or a missing start packet drops the frame in progress.
type VP8Depacketizer struct {
	buf      []byte
	started  bool
	keyframe bool
	lastSeq  uint16
	haveSeq  bool
}

func NewVP8Depacketizer() *VP8Depacketizer { return &VP8Depacketizer{} }

// Push feeds one RTP packet. It returns (frame, keyframe, true) once a packet
// carrying the marker bit completes an intact frame, and (nil, false, false)
// while more packets are expected or the packet had to be discarded.
func (d *VP8Depacketizer) Push(pkt *RtpPacket) (frame []byte, keyframe bool, complete bool) {
	if pkt == nil || len(pkt.Payload) < 1 {
		d.reset()
		return nil, false, false
	}

	seq := pkt.Header.SequenceNumber
	if d.started && d.haveSeq && seq != d.lastSeq+1 {
		// Lost a packet mid-frame: give up on it and wait for the next start.
		d.reset()
	}
	d.lastSeq = seq
	d.haveSeq = true

	hdrLen, ok := vp8DescriptorLen(pkt.Payload)
	if !ok {
		d.reset()
		return nil, false, false
	}
	body := pkt.Payload[hdrLen:]
	start := pkt.Payload[0]&0x10 != 0 && pkt.Payload[0]&0x07 == 0

	if start {
		d.buf = append(d.buf[:0], body...)
		d.started = true
		// RFC 6386 §9.1: bit 0 of the first VP8 payload byte is the inverse
		// key-frame flag (P). P==0 => key frame.
		d.keyframe = len(body) > 0 && body[0]&0x01 == 0
	} else {
		if !d.started {
			return nil, false, false
		}
		d.buf = append(d.buf, body...)
	}

	if pkt.Header.Marker && d.started {
		out := make([]byte, len(d.buf))
		copy(out, d.buf)
		kf := d.keyframe
		d.reset()
		return out, kf, true
	}
	return nil, false, false
}

func (d *VP8Depacketizer) reset() {
	d.buf = d.buf[:0]
	d.started = false
	d.keyframe = false
}

// vp8DescriptorLen returns the length of the VP8 payload descriptor at the
// start of an RTP payload (RFC 7741 §4.2), handling the extended (X=1) form.
func vp8DescriptorLen(payload []byte) (int, bool) {
	if len(payload) < 1 {
		return 0, false
	}
	if payload[0]&0x80 == 0 { // X=0: just the mandatory byte.
		return 1, true
	}
	if len(payload) < 2 {
		return 0, false
	}
	n := 2
	x := payload[1]
	if x&0x80 != 0 { // I: PictureID present.
		if len(payload) < n+1 {
			return 0, false
		}
		if payload[n]&0x80 != 0 { // M: 15-bit PictureID (2 bytes).
			n += 2
		} else {
			n++
		}
	}
	if x&0x40 != 0 { // L: TL0PICIDX.
		n++
	}
	if x&0x30 != 0 { // T or K: TID/KEYIDX byte.
		n++
	}
	if len(payload) < n {
		return 0, false
	}
	return n, true
}

// VideoTimestamp converts a millisecond capture time to a 90 kHz RTP timestamp.
func VideoTimestamp(ms uint32) uint32 {
	return uint32(uint64(ms) * core.WAVideoClockRate / 1000)
}
