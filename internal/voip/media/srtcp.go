package media

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"

	"wacalls/internal/voip/core"
)

// SRTCP session-key derivation labels (libsrtp), distinct from SRTP's 0x00/0x01/0x02.
const (
	srtcpLabelEncryption = 0x03
	srtcpLabelAuth       = 0x04
	srtcpLabelSalt       = 0x05
	srtcpAuthTagLen      = 10
	srtcpEncryptedFlag   = 0x80000000
	srtcpMaxIndex        = 0x7fffffff
)

// SrtcpContext protects outbound RTCP as WhatsApp SRTCP. It is keyed from the same per-call E2E
// master (core.SrtpKeyingMaterial, from DerivePerJidSrtpKey) as the audio SRTP, but expanded with
// the SRTCP labels; the audio path uses the SRTP labels on the same master.
type SrtcpContext struct {
	sessionKey  []byte
	sessionSalt []byte
	authKey     []byte
}

func NewSrtcpContext(keying core.SrtpKeyingMaterial) (*SrtcpContext, error) {
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
	return &SrtcpContext{sessionKey: sk, sessionSalt: ss, authKey: ak}, nil
}

// Protect wraps a plaintext RTCP compound packet into an SRTCP packet: the 8-byte RTCP header
// (V/P/RC/PT/length + sender SSRC) stays in the clear; bytes [8:] are AES-CTR encrypted with the
// SRTCP index as packet index; then a 4-byte word (E-flag | 31-bit index) and a 10-byte
// HMAC-SHA1-80 tag over everything preceding it are appended. index is the per-direction SRTCP
// counter, incremented once per packet sent.
func (c *SrtcpContext) Protect(rtcp []byte, index uint32) ([]byte, error) {
	if len(rtcp) < 8 {
		return nil, &SrtpError{SrtpErrPacketTooShort, fmt.Sprintf("rtcp too short: %d bytes", len(rtcp))}
	}
	senderSsrc := binary.BigEndian.Uint32(rtcp[4:8])
	n := len(rtcp)
	out := make([]byte, n+4+srtcpAuthTagLen)
	copy(out[:8], rtcp[:8])

	if n > 8 {
		iv := srtcpIV(c.sessionSalt, senderSsrc, uint64(index))
		if err := aesCtrXor(c.sessionKey, iv, rtcp[8:], out[8:n]); err != nil {
			return nil, &SrtpError{SrtpErrEncryption, err.Error()}
		}
	}

	binary.BigEndian.PutUint32(out[n:n+4], srtcpEncryptedFlag|(index&srtcpMaxIndex))

	mac := hmac.New(sha1.New, c.authKey)
	mac.Write(out[:n+4])
	copy(out[n+4:], mac.Sum(nil)[:srtcpAuthTagLen])
	return out, nil
}

// srtcpIV builds the AES-ICM nonce from the session salt, sender SSRC and SRTCP index, the same
// layout the E2E SRTP path uses with the RTP packet index (see SrtpContext.generateIV).
func srtcpIV(salt []byte, ssrc uint32, index uint64) []byte {
	iv := make([]byte, 16)
	copy(iv, salt[:14])

	var ssrcBuf [4]byte
	binary.BigEndian.PutUint32(ssrcBuf[:], ssrc)
	for i := range 4 {
		iv[4+i] ^= ssrcBuf[i]
	}

	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], index)
	for i := range 6 {
		iv[8+i] ^= idxBuf[2+i]
	}
	return iv
}
