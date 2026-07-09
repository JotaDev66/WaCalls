package call

import (
	"context"
	"log/slog"
	"testing"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/extension/audio"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type fakeSock struct{}

var _ core.VoipSocket = fakeSock{}

func (fakeSock) OwnPN() types.JID                                 { return types.JID{} }
func (fakeSock) OwnLID() types.JID                                { return types.JID{} }
func (fakeSock) AccountDeviceIdentityNode() (waBinary.Node, bool) { return waBinary.Node{}, false }
func (fakeSock) SendNode(ctx context.Context, node waBinary.Node) error {
	return nil
}
func (fakeSock) Query(ctx context.Context, node waBinary.Node) (*waBinary.Node, error) {
	return nil, nil
}
func (fakeSock) GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error) {
	return nil, nil
}
func (fakeSock) AssertSessions(ctx context.Context, jids []types.JID, force bool) error {
	return nil
}
func (fakeSock) CreateParticipantNodes(ctx context.Context, devices []types.JID, callKey []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool, error) {
	return nil, false, nil
}
func (fakeSock) DecryptCallKey(ctx context.Context, from types.JID, encChild *waBinary.Node) ([]byte, error) {
	return nil, nil
}
func (fakeSock) GetTCToken(ctx context.Context, jid types.JID) ([]byte, error) {
	return nil, nil
}
func (fakeSock) ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID {
	return pn
}

type fakeRelay struct {
	onData func([]byte)
}

var _ RelayTransport = (*fakeRelay)(nil)

func (r *fakeRelay) Broadcast(data []byte) {
	if r.onData != nil {
		r.onData(data)
	}
}
func (r *fakeRelay) HasConnection() bool                     { return true }
func (r *fakeRelay) SetSsrc(uint32)                          {}
func (r *fakeRelay) SetSubscriptionSsrc(uint32)              {}
func (r *fakeRelay) SetStreamSsrcs([]uint32, []uint32)       {}
func (r *fakeRelay) SetOnConnected(func(string, int))        {}
func (r *fakeRelay) SetOnReceive(func([]byte))               {}
func (r *fakeRelay) ResendSubscriptions()                    {}
func (r *fakeRelay) ConfigureRelays([]transport.RelayConfig) {}
func (r *fakeRelay) BufferedAmount() uint64                  { return 0 }
func (r *fakeRelay) ConnectedCount() int                     { return 1 }
func (r *fakeRelay) Cleanup()                                {}
func (r *fakeRelay) SetObserver(core.CallObserver)           {}

func km(seed byte) core.SrtpKeyingMaterial {
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

func TestMediaRoundtripThroughEngine(t *testing.T) {
	k1, k2 := km(1), km(9)

	recvCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("recv codec: %v", err)
	}
	recv := NewCallManager(fakeSock{}, slog.Default(), audio.New(recvCodec))
	recv.relay = &fakeRelay{}
	recv.srtp = engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	recv.selfSsrc = 2000
	recv.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	var got []float32
	recv.OnPeerAudio = func(pcm []float32) { got = pcm }
	recv.ensureExtensionsAttachedLocked("our.0", "peer.0")
	defer recv.cleanupMedia()

	sendCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("send codec: %v", err)
	}
	send := NewCallManager(fakeSock{}, slog.Default())
	send.relay = &fakeRelay{onData: recv.onRelayData}
	send.srtp = engine.NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	send.rtpSession = media.NewWhatsAppOpusSession(1000)
	send.selfSsrc = 1000
	send.debeEnabled = true

	frame := make([]float32, sendCodec.FrameSize())
	for i := range frame {
		frame[i] = float32((i%128)-64) / 128.0
	}
	enc, err := sendCodec.Encode(frame)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := send.sendAudioFrame(enc, sendCodec.FrameSize()); err != nil {
		t.Fatalf("sendAudioFrame: %v", err)
	}

	if len(got) == 0 {
		t.Fatal("peer audio was not delivered through the extracted engine path")
	}
}
