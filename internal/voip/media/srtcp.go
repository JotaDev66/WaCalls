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

// SrtcpAuthTagLen: HMAC-SHA1-80 (tag de 10 bytes), o padrão RFC 3711/libsrtp.
// A hipótese antiga de 20 bytes vinha só do tamanho de um SR ambíguo; agora
// confirmada por três pacotes reais capturados via -video-dump cujo campo
// "length" (sempre em claro, RFC 3550) prova o tamanho verdadeiro do RTCP
// original: um NACK de 30B (length=16B real) e um REMB de 42B (length=28B
// real) só fecham a conta com wire = claro(8) + corpo_cifrado + 4(E|index) +
// 10(tag); o mesmo tag=10 também fecha o compound SR(76B)+RR(32B)=108B a
// partir de um wire de 122B.
const SrtcpAuthTagLen = 10

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

// DebugKeyHex expõe a chave/salt/authKey derivados em hex, só pra log de
// depuração (-video-dump) — engenharia reversa offline do formato "fast" do
// WhatsApp precisa desse material pra testar hipóteses sem precisar de uma
// ligação nova a cada tentativa. Efêmero (por chamada), não persiste nada.
func (c *SrtcpContext) DebugKeyHex() (key, salt, authKey string) {
	return hexEnc(c.sessionKey), hexEnc(c.sessionSalt), hexEnc(c.authKey)
}

func hexEnc(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0xf]
	}
	return string(out)
}

// srtcpIV monta o IV do CTR: salt (14B) preenchido a 16, XOR do SSRC nos bytes
// 4-7 e XOR do índice SRTCP (até 31 bits) nos bytes 8-13, sem deslocamento.
//
// BUG CORRIGIDO (2026-09-17): a versão anterior fazia `uint64(index) << 16`
// antes de colocar nos bytes 8-13 — um "<<16" a mais que não existe no RFC
// 3711 nem no SRTP deste mesmo projeto (compare com `generateIV` em
// srtp.go, que usa o índice de 48 bits DIRETO, sem chocá-lo). Esse
// deslocamento extra deixava a chave "certa" produzindo IV errado sempre,
// o que por meses pareceu "o WhatsApp usa outra chave" quando na verdade
// era só isso — confirmado comparando com a implementação de referência
// (github.com/purpshell/meowcaller, BuildE2eRtpIV/CryptPayload) e revertido
// pra bater com o padrão RFC 3711 (índice ocupa os 48 bits direto, não
// deslocado). Isso quebra retrocompatibilidade com qualquer coisa que já
// dependesse do IV errado (nada deveria depender, já que nunca decifrou
// nada de verdade).
func (c *SrtcpContext) srtcpIV(ssrc, index uint32) []byte {
	iv := make([]byte, 16)
	copy(iv, c.sessionSalt[:14])

	var s [4]byte
	binary.BigEndian.PutUint32(s[:], ssrc)
	for i := 0; i < 4; i++ {
		iv[4+i] ^= s[i]
	}

	var idx [8]byte
	binary.BigEndian.PutUint64(idx[:], uint64(index))
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

// ErrAuthTagMismatch sinaliza que o tag de autenticação não bateu. Ao
// contrário de outros erros de Unprotect, este vem acompanhado do pacote
// decifrado mesmo assim (não bloqueia o processamento) — a derivação da
// chave de auth do canal "fast" de feedback do WhatsApp (NACK) ainda não
// está confirmada, então rejeitar esses pacotes hoje quebraria a
// retransmissão de vídeo. Quem chama decide se loga/investiga; ver
// callmanager_rtcp.go.
var ErrAuthTagMismatch = errors.New("srtcp: auth tag inválido")

// Unprotect descifra um SRTCP e devolve o pacote RTCP composto em claro.
// Valida o tag de autenticação, mas um tag inválido não impede o
// processamento: devolve o pacote decifrado mesmo assim, junto com
// ErrAuthTagMismatch, para não bloquear NACK/PLI enquanto a derivação de
// chave do canal "fast" do WhatsApp segue sendo confirmada. UnprotectWithSSRC
// continua sem essa checagem — é ferramenta de diagnóstico (-video-dump) que
// testa SSRCs candidatos de propósito.
func (c *SrtcpContext) Unprotect(data []byte) ([]byte, error) {
	authErr := c.verifyAuthTag(data)
	plain, err := c.UnprotectWithSSRC(data, 0, false)
	if err != nil {
		return nil, err
	}
	if authErr != nil {
		return plain, ErrAuthTagMismatch
	}
	return plain, nil
}

func (c *SrtcpContext) verifyAuthTag(data []byte) error {
	if len(data) < c.authTagLen {
		return errors.New("srtcp: pacote curto demais pro tag")
	}
	body := data[:len(data)-c.authTagLen]
	wantTag := data[len(data)-c.authTagLen:]
	gotTag := hmacSha1Trunc(c.authKey, body, c.authTagLen)
	if !hmac.Equal(gotTag, wantTag) {
		return errors.New("srtcp: auth tag inválido")
	}
	return nil
}

// UnprotectWithSSRC é igual a Unprotect, mas permite forçar o SSRC usado na
// derivação do IV em vez do "sender SSRC" que vem em claro no próprio pacote
// (useOverride=true). Existe pra diagnosticar empiricamente, via -video-dump,
// se o canal de feedback "fast" do WhatsApp deriva o IV de um SSRC diferente
// do que ele mesmo anuncia no cabeçalho (ex.: sempre "1" nos NACKs vistos).
func (c *SrtcpContext) UnprotectWithSSRC(data []byte, ssrcOverride uint32, useOverride bool) ([]byte, error) {
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
		if useOverride {
			ssrc = ssrcOverride
		}
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
