package call

import (
	"log/slog"
	"sync"
	"testing"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

type audioRelay struct {
	mu   sync.Mutex
	sent [][]byte
}

func (r *audioRelay) SetSsrc(uint32)                          {}
func (r *audioRelay) SetSubscriptionSsrc(uint32)              {}
func (r *audioRelay) SetStreamSsrcs([]uint32, []uint32)       {}
func (r *audioRelay) SetOnConnected(func(string, int))        {}
func (r *audioRelay) SetOnReceive(func([]byte))               {}
func (r *audioRelay) ResendSubscriptions()                    {}
func (r *audioRelay) ConfigureRelays([]transport.RelayConfig) {}
func (r *audioRelay) BufferedAmount() uint64                  { return 0 }
func (r *audioRelay) HasConnection() bool                     { return true }
func (r *audioRelay) ConnectedCount() int                     { return 1 }
func (r *audioRelay) Cleanup()                                {}
func (r *audioRelay) Broadcast(d []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, append([]byte(nil), d...))
}

func audioKM(seed byte) core.SrtpKeyingMaterial {
	mk := make([]byte, 16)
	ms := make([]byte, 14)
	for i := range mk {
		mk[i] = seed + byte(i)
	}
	for i := range ms {
		ms[i] = seed*2 + byte(i)
	}
	return core.SrtpKeyingMaterial{MasterKey: mk, MasterSalt: ms}
}

func TestAudioRoundtripThroughCallManager(t *testing.T) {
	k1, k2 := audioKM(1), audioKM(9)
	senderCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("sender codec: %v", err)
	}
	receiverCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("receiver codec: %v", err)
	}

	const senderSsrc, receiverSsrc = uint32(1000), uint32(2000)
	relay := &audioRelay{}
	sender := &CallManager{
		log:         slog.Default(),
		relay:       relay,
		srtp:        engine.NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen),
		rtpSession:  media.NewWhatsAppOpusSession(senderSsrc),
		codec:       senderCodec,
		selfSsrc:    senderSsrc,
		debeEnabled: true,
	}

	frame := make([]float32, senderCodec.FrameSize())
	for i := range frame {
		frame[i] = float32((i%128)-64) / 128.0
	}
	encoded, err := senderCodec.Encode(frame)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	sender.mu.Lock()
	sender.sendOpusFrameLocked(encoded)
	sender.mu.Unlock()

	relay.mu.Lock()
	pkts := append([][]byte(nil), relay.sent...)
	relay.mu.Unlock()
	if len(pkts) == 0 {
		t.Fatal("sender produced no SRTP packet")
	}

	var gotPCM []float32
	receiver := &CallManager{
		log:         slog.Default(),
		relay:       &audioRelay{},
		srtp:        engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen),
		codec:       receiverCodec,
		selfSsrc:    receiverSsrc,
		OnPeerAudio: func(pcm []float32) { gotPCM = pcm },
	}
	receiver.handleAudioRelayData(pkts[0])
	if len(gotPCM) == 0 {
		t.Fatal("receiver did not decode peer audio from the roundtripped SRTP packet")
	}
}
