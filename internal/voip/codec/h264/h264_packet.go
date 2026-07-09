package h264

const (
	h264StapAType = 24
	h264FuaType   = 28
	mtuPayloadMax = 800

	maxFuReassemblyBytes = 4 << 20
)

func PackageH264NALU(nalu []byte) [][]byte {
	if len(nalu) == 0 {
		return nil
	}

	if len(nalu) <= mtuPayloadMax {
		out := make([]byte, len(nalu))
		copy(out, nalu)
		return [][]byte{out}
	}
	naluHeader := nalu[0]
	fbitAndNri := naluHeader & 0xE0
	originalType := naluHeader & 0x1F
	fuIndicator := fbitAndNri | h264FuaType

	body := nalu[1:]
	fragSize := mtuPayloadMax - 2

	var out [][]byte
	offset := 0
	for offset < len(body) {
		end := offset + fragSize
		if end > len(body) {
			end = len(body)
		}
		chunk := body[offset:end]

		fuHeader := originalType
		if offset == 0 {
			fuHeader |= 0x80
		}
		if end == len(body) {
			fuHeader |= 0x40
		}

		pkt := make([]byte, 2+len(chunk))
		pkt[0] = fuIndicator
		pkt[1] = fuHeader
		copy(pkt[2:], chunk)
		out = append(out, pkt)

		offset = end
	}
	return out
}

func PackageH264STAPA(nalus [][]byte) []byte {
	if len(nalus) < 2 {
		return nil
	}
	total := 1
	var maxNri byte
	for _, n := range nalus {
		if len(n) == 0 || len(n) > 0xFFFF {
			return nil
		}
		total += 2 + len(n)
		if total > mtuPayloadMax {
			return nil
		}
		nri := n[0] & 0x60
		if nri > maxNri {
			maxNri = nri
		}
	}

	out := make([]byte, total)
	out[0] = maxNri | h264StapAType
	offset := 1
	for _, n := range nalus {
		out[offset] = byte(len(n) >> 8)
		out[offset+1] = byte(len(n))
		offset += 2
		copy(out[offset:], n)
		offset += len(n)
	}
	return out
}

func SplitAnnexB(data []byte) [][]byte {
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

func annexBStartCodeLen(data []byte, offset int) int {
	if offset+3 < len(data) &&
		data[offset] == 0 && data[offset+1] == 0 &&
		data[offset+2] == 0 && data[offset+3] == 1 {
		return 4
	}
	if offset+2 < len(data) &&
		data[offset] == 0 && data[offset+1] == 0 && data[offset+2] == 1 {
		return 3
	}
	return 0
}

type H264Depacketizer struct {
	fuBuf    []byte
	fuType   byte
	fuNriF   byte
	fuActive bool
}

func (d *H264Depacketizer) Depacketize(payload []byte) [][]byte {
	if len(payload) < 1 {
		return nil
	}
	naluType := payload[0] & 0x1F
	fbitAndNri := payload[0] & 0xE0

	switch {
	case naluType >= 1 && naluType <= 23:
		d.fuActive = false
		out := make([]byte, len(payload))
		copy(out, payload)
		return [][]byte{out}

	case naluType == h264StapAType:
		d.fuActive = false
		body := payload[1:]
		var out [][]byte
		for len(body) >= 2 {
			size := int(body[0])<<8 | int(body[1])
			body = body[2:]
			if size <= 0 || size > len(body) {
				return out
			}
			nalu := make([]byte, size)
			copy(nalu, body[:size])
			out = append(out, nalu)
			body = body[size:]
		}
		return out

	case naluType == h264FuaType:
		if len(payload) < 2 {
			d.fuActive = false
			return nil
		}
		fuHeader := payload[1]
		startBit := fuHeader & 0x80
		endBit := fuHeader & 0x40
		origType := fuHeader & 0x1F
		body := payload[2:]

		if startBit != 0 {
			d.fuActive = true
			d.fuType = origType
			d.fuNriF = fbitAndNri
			d.fuBuf = append(d.fuBuf[:0], fbitAndNri|origType)
			d.fuBuf = append(d.fuBuf, body...)
		} else if d.fuActive {
			if len(d.fuBuf)+len(body) > maxFuReassemblyBytes {
				d.fuActive = false
				d.fuBuf = d.fuBuf[:0]
				return nil
			}
			d.fuBuf = append(d.fuBuf, body...)
		} else {
			return nil
		}

		if endBit != 0 && d.fuActive {
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
