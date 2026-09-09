package media

import "encoding/binary"

// VideoFrame é um quadro H264 (Annex-B) codificado, trocado com o browser pelo
// data channel "vp8". O browser codifica/decodifica com WebCodecs; o lado Go só
// empacota em RTP, então o bitstream do codec é opaco aqui.
type VideoFrame struct {
	Keyframe    bool
	TimestampMS uint32
	// Rotation é a orientação do quadro em graus no sentido horário
	// (0, 90, 180 ou 270), vinda da extensão RTP CVO do WhatsApp. O browser
	// gira o desenho no canvas de acordo.
	Rotation uint16
	Data     []byte
}

// videoFrameHeaderLen é o prefixo fixo de toda mensagem do data channel "vp8":
// 1 byte de flags + 4 bytes de timestamp em ms (big-endian).
//
// Flags: bit 0 = keyframe; bits 1-2 = código de rotação (0=0°, 1=90°, 2=180°,
// 3=270°).
const videoFrameHeaderLen = 5

func rotationToCode(deg uint16) byte {
	switch deg {
	case 90:
		return 1
	case 180:
		return 2
	case 270:
		return 3
	default:
		return 0
	}
}

func codeToRotation(code byte) uint16 {
	switch code & 0x03 {
	case 1:
		return 90
	case 2:
		return 180
	case 3:
		return 270
	default:
		return 0
	}
}

// EncodeVideoFrame serializa um quadro para o data channel.
func EncodeVideoFrame(f VideoFrame) []byte {
	out := make([]byte, videoFrameHeaderLen+len(f.Data))
	if f.Keyframe {
		out[0] |= 1
	}
	out[0] |= rotationToCode(f.Rotation) << 1
	binary.BigEndian.PutUint32(out[1:], f.TimestampMS)
	copy(out[videoFrameHeaderLen:], f.Data)
	return out
}

// DecodeVideoFrame faz o parse de uma mensagem do data channel. ok é false
// quando a mensagem é curta demais para o cabeçalho.
func DecodeVideoFrame(b []byte) (f VideoFrame, ok bool) {
	if len(b) < videoFrameHeaderLen {
		return VideoFrame{}, false
	}
	f.Keyframe = b[0]&1 != 0
	f.Rotation = codeToRotation(b[0] >> 1)
	f.TimestampMS = binary.BigEndian.Uint32(b[1:])
	f.Data = append([]byte(nil), b[videoFrameHeaderLen:]...)
	return f, true
}
