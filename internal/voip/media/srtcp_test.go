package media

import (
	"bytes"
	"errors"
	"testing"

	"wacalls/internal/voip/core"
)

func TestSrtcpRoundTrip(t *testing.T) {
	km, err := DerivePerJidSrtpKey(make([]byte, 32), "dev:0@s.whatsapp.net")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	send, err := NewSrtcpContext(km, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatalf("send ctx: %v", err)
	}
	recv, err := NewSrtcpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatalf("recv ctx: %v", err)
	}

	compound := append(BuildSenderReport(0xAABBCCDD, 12345, 10, 4000),
		BuildREMB(0xAABBCCDD, 0x11223344, 900_000)...)

	enc, err := send.Protect(compound)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	if len(enc) <= len(compound) {
		t.Fatalf("SRTCP não cresceu (índice+tag): %d vs %d", len(enc), len(compound))
	}
	if !IsRTCP(enc) {
		t.Fatal("IsRTCP falso para SRTCP")
	}

	dec, err := recv.Unprotect(enc)
	if err != nil {
		t.Fatalf("unprotect: %v", err)
	}
	if !bytes.Equal(dec, compound) {
		t.Fatalf("compound mudou no round trip: %x vs %x", dec, compound)
	}

	infos := ParseRTCPCompound(dec)
	if len(infos) != 2 || infos[0].Name != "SR" || infos[1].Name != "PSFB/AFB(REMB)" {
		t.Fatalf("parse errado: %+v", infos)
	}
	if infos[0].SSRC != 0xAABBCCDD {
		t.Fatalf("SSRC do SR errado: %#x", infos[0].SSRC)
	}
}

func TestSrtcpUnprotectRejectsTamperedTag(t *testing.T) {
	km, err := DerivePerJidSrtpKey(make([]byte, 32), "dev:0@s.whatsapp.net")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	send, err := NewSrtcpContext(km, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatalf("send ctx: %v", err)
	}
	recv, err := NewSrtcpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatalf("recv ctx: %v", err)
	}

	compound := append(BuildSenderReport(0xAABBCCDD, 12345, 10, 4000),
		BuildREMB(0xAABBCCDD, 0x11223344, 900_000)...)

	enc, err := send.Protect(compound)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}

	tampered := append([]byte(nil), enc...)
	tampered[len(tampered)-1] ^= 0xFF

	// Unprotect sinaliza a tag inválida via ErrAuthTagMismatch, mas não
	// bloqueia o processamento — ainda devolve o pacote decifrado. A chave
	// de auth do canal "fast" (NACK) do WhatsApp ainda não está confirmada,
	// então bloquear aqui quebraria a retransmissão de vídeo real (ver
	// callmanager_rtcp.go, que loga esse caso mas continua processando).
	plain, err := recv.Unprotect(tampered)
	if !errors.Is(err, ErrAuthTagMismatch) {
		t.Fatalf("Unprotect deve sinalizar ErrAuthTagMismatch numa tag adulterada, veio: %v", err)
	}
	if plain == nil {
		t.Fatal("Unprotect deve devolver o pacote decifrado mesmo com tag inválida, não bloquear")
	}

	// UnprotectWithSSRC is the diagnostic bypass and must NOT verify the
	// tag — it should still "succeed" (decrypt to garbage, no auth error)
	// even on the tampered packet, by design.
	if _, err := recv.UnprotectWithSSRC(tampered, 0, false); err != nil {
		t.Fatalf("UnprotectWithSSRC must not enforce auth (diagnostic bypass), got: %v", err)
	}
}

func TestBuildREMBBitrate(t *testing.T) {
	// 1.5 Mbps deve caber e reconstruir aproximadamente.
	b := BuildREMB(1, 2, 1_500_000)
	if string(b[12:16]) != "REMB" {
		t.Fatalf("faltou tag REMB: %x", b[12:16])
	}
	exp := uint32(b[17] >> 2)
	mant := (uint64(b[17]&0x03) << 16) | (uint64(b[18]) << 8) | uint64(b[19])
	got := mant << exp
	if got < 1_400_000 || got > 1_600_000 {
		t.Fatalf("bitrate reconstruído fora de faixa: %d", got)
	}
}
