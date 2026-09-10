package media

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"errors"

	"wacalls/internal/voip/core"
)

// Labels do KDF para SRTCP (RFC 3711 §4.3.2), distintos dos de SRTP (0/1/2).
const (
	srtcpLabelEncryption = 0x03
	srtcpLabelAuth       = 0x04
	srtcpLabelSalt       = 0x05
)

// SrtcpAuthTagLen: o WhatsApp usa a tag HMAC-SHA1 completa (20 bytes) no SRTCP,
// diferente do SRTP deste projeto (4). Deduzido do tamanho do SR recebido:
// 52 (SR c/ 1 report block) + 4 (E|index) + 20 = 76.
const SrtcpAuthTagLen = 20

// SrtcpContext protege/desprotege pacotes RTCP compostos (SRTCP, RFC 3711 §3.4)
// com o mesmo material de chave do SRTP, só que pelos labels 3/4/5. AES-CTR +
// HMAC-SHA1 truncado, igual à perna SRTP deste projeto.
type SrtcpContext struct {
	sessionKey  []byte // 16
	sessionSalt []byte // 14
	authKey     []byte // 20
	authTagLen  int
	sendIndex   uint32 // contador de 31 bits do lado de envio
}

func NewSrtcpContext(keying core.SrtpKeyingMaterial, authTagLen int) (*SrtcpContext, error) {
	if authTagLen <= 0 {
		authTagLen = core.SRTPAuthTagLen
	}
	sk, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, srtcpLabelEncryption, 16)
	if err != nil {
		return nil, err
	}
	ak, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, srtcpLabelAuth, 20)
	if err != nil {
		return nil, err
	}
	ss, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, srtcpLabelSalt, 14)
	if err != nil {
		return nil, err
	}
	return &SrtcpContext{sessionKey: sk, sessionSalt: ss, authKey: ak, authTagLen: authTagLen}, nil
}

// srtcpIV monta o IV do CTR: salt (14B) preenchido a 16, XOR do SSRC nos bytes
// 4-7 e XOR de (index << 16) nos bytes 8-13 — mesma forma do generateIV do SRTP.
func (c *SrtcpContext) srtcpIV(ssrc, index uint32) []byte {
	iv := make([]byte, 16)
	copy(iv, c.sessionSalt[:14])

	var s [4]byte
	binary.BigEndian.PutUint32(s[:], ssrc)
	for i := 0; i < 4; i++ {
		iv[4+i] ^= s[i]
	}

	var idx [8]byte
	binary.BigEndian.PutUint64(idx[:], uint64(index)<<16)
	for i := 0; i < 6; i++ {
		iv[8+i] ^= idx[2+i]
	}
	return iv
}

func hmacSha1Trunc(key, data []byte, n int) []byte {
	mac := hmac.New(sha1.New, key)
	mac.Write(data)
	sum := mac.Sum(nil)
	if n > len(sum) {
		n = len(sum)
	}
	return sum[:n]
}

// Protect recebe um pacote RTCP composto em claro e devolve o SRTCP:
//
//	[8 bytes de header+SSRC em claro] [resto cifrado] [E(1)|SRTCP index(31)] [tag]
func (c *SrtcpContext) Protect(rtcp []byte) ([]byte, error) {
	if len(rtcp) < 8 {
		return nil, errors.New("srtcp: pacote RTCP curto demais")
	}
	c.sendIndex = (c.sendIndex + 1) & 0x7fffffff
	ssrc := binary.BigEndian.Uint32(rtcp[4:8])

	out := make([]byte, len(rtcp)+4+c.authTagLen)
	copy(out[:8], rtcp[:8])

	iv := c.srtcpIV(ssrc, c.sendIndex)
	if err := aesCtrXor(c.sessionKey, iv, rtcp[8:], out[8:len(rtcp)]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(out[len(rtcp):], c.sendIndex|0x80000000) // E=1

	tag := hmacSha1Trunc(c.authKey, out[:len(rtcp)+4], c.authTagLen)
	copy(out[len(rtcp)+4:], tag)
	return out, nil
}

// Unprotect descifra um SRTCP e devolve o pacote RTCP composto em claro. Não
// valida o tag de auth (o transporte já é confiável); só descifra.
func (c *SrtcpContext) Unprotect(data []byte) ([]byte, error) {
	if len(data) < 8+4+c.authTagLen {
		return nil, errors.New("srtcp: pacote curto demais")
	}
	body := data[:len(data)-c.authTagLen]
	eIdx := binary.BigEndian.Uint32(body[len(body)-4:])
	encrypted := eIdx&0x80000000 != 0
	index := eIdx & 0x7fffffff
	rtcpPart := body[:len(body)-4]
	if len(rtcpPart) < 8 {
		return nil, errors.New("srtcp: parte RTCP curta demais")
	}

	out := make([]byte, len(rtcpPart))
	copy(out[:8], rtcpPart[:8])
	if encrypted {
		ssrc := binary.BigEndian.Uint32(rtcpPart[4:8])
		iv := c.srtcpIV(ssrc, index)
		if err := aesCtrXor(c.sessionKey, iv, rtcpPart[8:], out[8:]); err != nil {
			return nil, err
		}
	} else {
		copy(out[8:], rtcpPart[8:])
	}
	return out, nil
}

// IsRTCP reconhece um pacote RTCP (ou SRTCP) pelo segundo byte: os payload types
// de RTCP ficam em 192..223, faixa que não colide com os PTs de RTP usados aqui
// (96/97/120, com ou sem marker bit).
func IsRTCP(data []byte) bool {
	return len(data) >= 2 && data[1] >= 192 && data[1] <= 223
}
