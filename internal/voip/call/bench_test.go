package call

import (
	"log/slog"
	"testing"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

func BenchmarkSendAudioFrameNopObserver(b *testing.B)      { benchSendFrame(b, core.NopObserver{}) }
func BenchmarkSendAudioFrameCountingObserver(b *testing.B) { benchSendFrame(b, &countingObserver{}) }

func benchSendFrame(b *testing.B, obs core.CallObserver) {
	codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		b.Fatalf("codec: %v", err)
	}
	cm := NewCallManager(fakeSock{}, slog.Default())
	cm.observer = obs
	cm.relay = &fakeRelay{}
	cm.srtp = engine.NewSrtpManager(km(1), km(9), core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	cm.srtp.SetObserver(obs)
	cm.selfSsrc = 1000
	cm.rtpSession = media.NewWhatsAppOpusSession(1000)

	frame := make([]float32, codec.FrameSize())
	enc, err := codec.Encode(frame)
	if err != nil {
		b.Fatalf("encode: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cm.sendAudioFrame(enc, codec.FrameSize())
	}
}
