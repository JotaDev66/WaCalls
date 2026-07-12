package call

import (
	"context"
	"encoding/hex"
	"log/slog"
	"math"
	"testing"

	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/codec/opus"
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
	onData      func([]byte)
	onConfigure func([]transport.RelayConfig)
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
func (r *fakeRelay) SetOnConnected(func(string, int)) {}
func (r *fakeRelay) SetOnReceive(func([]byte))        {}
func (r *fakeRelay) SetOnUsableChange(func(int))      {}
func (r *fakeRelay) ResendSubscriptions()             {}
func (r *fakeRelay) ConfigureRelays(relays []transport.RelayConfig) {
	if r.onConfigure != nil {
		r.onConfigure(relays)
	}
}
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

// One real 20 ms CELT-FB mono frame (TOC 0xF8) of a 440 Hz tone, encoded with
// ffmpeg/libopus; same provenance as the vectors in codec/opus/decoder_test.go.
const stdOpusFrameHex = "f8b04e7f9fc6d1ed077136f42f3ac682c4f17fde50d7458761fb93aa48ecde8240596372c8ae1d51b1dd0782c428b28459b8fdf58db57179cc2a4a2140ed68513d0b06d9a06c01066356124a46776549897d0d4e9d0b087c292b726822654ddb30e2ce12decc90499e11d89b3cc38bcc8737e75b97591166d6427026673e4744864e0c829faf2cc88a7598590afb45da51b9f3c1187ed6317dd5db1ba38d4fee"

// A peer without MLow sends standard Opus on the same PT 120 stream; the full
// recv path (RTP parse, SRTP unprotect, extension routing, fallback decode)
// must deliver audible PCM, not silence.
func TestStandardOpusRoundtripThroughEngine(t *testing.T) {
	k1, k2 := km(1), km(9)

	recvCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("recv codec: %v", err)
	}
	recv := NewCallManager(fakeSock{}, slog.Default(), audio.New(opus.WithFallback(recvCodec)))
	recv.relay = &fakeRelay{}
	recv.srtp = engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	recv.selfSsrc = 2000
	recv.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	var got []float32
	recv.OnPeerAudio = func(pcm []float32) { got = pcm }
	recv.ensureExtensionsAttachedLocked("our.0", "peer.0")
	defer recv.cleanupMedia()

	send := NewCallManager(fakeSock{}, slog.Default())
	send.relay = &fakeRelay{onData: recv.onRelayData}
	send.srtp = engine.NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	send.rtpSession = media.NewWhatsAppOpusSession(1000)
	send.selfSsrc = 1000

	frame, err := hex.DecodeString(stdOpusFrameHex)
	if err != nil {
		t.Fatalf("bad vector: %v", err)
	}
	if err := send.sendAudioFrame(frame, 320); err != nil {
		t.Fatalf("sendAudioFrame: %v", err)
	}

	if len(got) != 320 {
		t.Fatalf("got %d samples, want 320 (20 ms @ 16 kHz)", len(got))
	}
	var sum float64
	for _, s := range got {
		sum += float64(s) * float64(s)
	}
	if rms := math.Sqrt(sum / float64(len(got))); rms < 0.01 {
		t.Fatalf("rms %.5f, want > 0.01: standard-opus frame must arrive as audio, not silence", rms)
	}
}

func TestInboundRtcpNotTreatedAsAudio(t *testing.T) {
	k1, k2 := km(1), km(9)
	recvCodec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatalf("recv codec: %v", err)
	}
	recv := NewCallManager(fakeSock{}, slog.Default(), audio.New(opus.WithFallback(recvCodec)))
	recv.relay = &fakeRelay{}
	recv.srtp = engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	recv.selfSsrc = 2000
	recv.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	var got []float32
	recv.OnPeerAudio = func(pcm []float32) { got = pcm }
	recv.ensureExtensionsAttachedLocked("our.0", "peer.0")
	defer recv.cleanupMedia()

	// RTCP framing (first byte 0x80) with a second byte 0xF8 whose masked PT
	// (0xF8 & 0x7f = 120) collides with the audio payload type. Before the fix
	// this reached the RTP path and mutated peer subscription state.
	pkt := make([]byte, 28)
	pkt[0] = 0x80
	pkt[1] = 0xF8
	recv.onRelayData(pkt)

	if recv.actualPeerSet {
		t.Fatal("inbound rtcp must not set peer subscription state")
	}
	if got != nil {
		t.Fatal("inbound rtcp must not be delivered as audio")
	}
}
