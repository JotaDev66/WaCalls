package call

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

// fakeRelay is a RelayTransport that records what was broadcast so a test can
// feed it back in, standing in for the round trip through a real relay.
type fakeRelay struct {
	connected bool
	sent      [][]byte
	onReceive func([]byte)
}

func (f *fakeRelay) SetSsrc(uint32)                          {}
func (f *fakeRelay) SetSubscriptionSsrc(uint32)              {}
func (f *fakeRelay) SetVideoSsrc(uint32)                     {}
func (f *fakeRelay) SetVideoSubscriptionSsrc(uint32)         {}
func (f *fakeRelay) SetOnConnected(func(string, int))        {}
func (f *fakeRelay) SetOnReceive(fn func([]byte))            { f.onReceive = fn }
func (f *fakeRelay) ResendSubscriptions()                    {}
func (f *fakeRelay) ConfigureRelays([]transport.RelayConfig) {}
func (f *fakeRelay) Broadcast(data []byte)                   { f.sent = append(f.sent, append([]byte(nil), data...)) }
func (f *fakeRelay) HasConnection() bool                     { return f.connected }
func (f *fakeRelay) ConnectedCount() int                     { return 1 }
func (f *fakeRelay) Cleanup()                                {}

// TestVideoLoopbackThroughSRTP prova que o pipeline de vídeo de saída
// (payloader H264 -> RTP -> SRTP protect) e o de entrada
// (SRTP unprotect -> depacketizer H264 -> OnPeerVideo) se encaixam: uma unidade
// de acesso Annex-B entregue a FeedCapturedVideo volta por OnPeerVideo sem
// mudanças depois de um round trip pelo relay (fake). É o análogo do loopback de
// áudio no plano de mídia; NÃO exercita o relay real do WhatsApp.
func TestVideoLoopbackThroughSRTP(t *testing.T) {
	m := &CallManager{log: slog.Default()}
	fr := &fakeRelay{connected: true}
	m.relay = fr

	m.currentCall = NewOutgoingCall("CID", "peer@s.whatsapp.net", "me@s.whatsapp.net", core.CallMediaTypeVideo)
	m.initVideoLocked("CID", "me@s.whatsapp.net", "peer@s.whatsapp.net")

	// Identical send/recv keying so a self-protected packet unprotects locally.
	km, err := media.DerivePerJidSrtpKey(make([]byte, 32), "dev:0@s.whatsapp.net")
	if err != nil {
		t.Fatalf("derive srtp key: %v", err)
	}
	m.srtpSession, err = media.NewSrtpSession(km, km, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatalf("new srtp session: %v", err)
	}

	got := make(chan media.VideoFrame, 4)
	m.OnPeerVideo = func(f media.VideoFrame) { got <- f }

	// Unidade de acesso Annex-B: um NALU SPS pequeno (tipo 7) + um NALU IDR
	// grande (tipo 5, o bastante para forçar fragmentação FU-A). Start codes de
	// 4 bytes, sem NALU terminando em 0x00 (splitAnnexB corta zeros no fim).
	sc := []byte{0x00, 0x00, 0x00, 0x01}
	sps := append([]byte{0x67}, bytes.Repeat([]byte{0x42}, 12)...)
	idr := append([]byte{0x65}, bytes.Repeat([]byte{0x42}, media.MaxH264PayloadSize+64)...)
	orig := append(append(append(append([]byte{}, sc...), sps...), sc...), idr...)
	m.FeedCapturedVideo(media.VideoFrame{Keyframe: true, TimestampMS: 40, Data: orig})

	if len(fr.sent) < 2 {
		t.Fatalf("expected the frame to be broadcast as >=2 SRTP packets, got %d", len(fr.sent))
	}

	// Feed the broadcast packets back in. Clear selfVideoSsrc so onRelayData
	// does not discard them as our own loopback.
	m.selfVideoSsrc = 0
	for _, p := range fr.sent {
		m.onRelayData(p)
	}

	select {
	case f := <-got:
		if !f.Keyframe {
			t.Error("keyframe flag lost in the round trip")
		}
		if !bytes.Equal(f.Data, orig) {
			t.Fatalf("frame changed in the round trip: got %d bytes, want %d", len(f.Data), len(orig))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnPeerVideo never fired")
	}
}

// TestFeedCapturedVideoNoopWithoutVideoPlane garante que uma chamada de áudio-só
// (sem payloader) ignora quadros de vídeo em silêncio em vez de dar panic.
func TestFeedCapturedVideoNoopWithoutVideoPlane(t *testing.T) {
	m := &CallManager{log: slog.Default()}
	m.relay = &fakeRelay{connected: true}
	m.currentCall = NewOutgoingCall("CID", "peer@s.whatsapp.net", "me@s.whatsapp.net", core.CallMediaTypeAudio)

	m.FeedCapturedVideo(media.VideoFrame{Keyframe: true, TimestampMS: 1, Data: []byte{1, 2, 3}})
	// no panic, nothing broadcast
	if fr := m.relay.(*fakeRelay); len(fr.sent) != 0 {
		t.Fatalf("audio call should not broadcast video, got %d packets", len(fr.sent))
	}
}
