package video

import (
	"bytes"
	"sync"
	"testing"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/transport"
)

type fakeRelay struct {
	mu         sync.Mutex
	broadcasts int
	connected  bool
	buffered   uint64
}

func (f *fakeRelay) Broadcast(data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts++
}
func (f *fakeRelay) BufferedAmount() uint64             { return f.buffered }
func (f *fakeRelay) HasConnection() bool                { return f.connected }
func (f *fakeRelay) SetStreamSsrcs(self, peer []uint32) {}

func TestFeedCapturedNoopBeforeSetup(t *testing.T) {
	r := &fakeRelay{connected: true}
	p := New(nil, r)
	p.FeedCaptured([]byte{0, 0, 0, 1, 0x65, 0x01, 0x02})
	if r.broadcasts != 0 {
		t.Fatalf("expected no broadcast before Setup, got %d", r.broadcasts)
	}
}

func TestHandleRelayDataNoopBeforeSetup(t *testing.T) {
	r := &fakeRelay{connected: true}
	p := New(nil, r)
	emitted := false
	p.OnFrame = func([]byte) { emitted = true }
	p.HandleRelayData(make([]byte, 16))
	if emitted {
		t.Fatal("expected no frame emitted before Setup")
	}
}

func TestResetClearsState(t *testing.T) {
	p := New(nil, &fakeRelay{})
	p.depack = &transport.H264Depacketizer{}
	p.frameBuf = []byte{1, 2, 3}
	p.selfSsrc = 42
	p.Reset()
	if p.depack != nil || p.frameBuf != nil || p.selfSsrc != 0 {
		t.Fatal("Reset did not clear pipeline state")
	}
}

type captureRelay struct {
	mu        sync.Mutex
	sent      [][]byte
	connected bool
}

func (r *captureRelay) Broadcast(d []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, append([]byte(nil), d...))
}
func (r *captureRelay) BufferedAmount() uint64             { return 0 }
func (r *captureRelay) HasConnection() bool                { return r.connected }
func (r *captureRelay) SetStreamSsrcs(self, peer []uint32) {}

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

func TestVideoRoundtripThroughSharedManager(t *testing.T) {
	k1, k2 := km(1), km(9)
	senderMgr := engine.NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	receiverMgr := engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)

	sRelay := &captureRelay{connected: true}
	sender := New(nil, sRelay)
	if err := sender.Setup("call1", "alice.0", "bob.0", senderMgr); err != nil {
		t.Fatalf("sender setup: %v", err)
	}

	rRelay := &captureRelay{connected: true}
	receiver := New(nil, rRelay)
	if err := receiver.Setup("call1", "bob.0", "alice.0", receiverMgr); err != nil {
		t.Fatalf("receiver setup: %v", err)
	}

	var got []byte
	receiver.OnFrame = func(au []byte) { got = au }

	au := []byte{0, 0, 0, 1, 0x67, 0xAA, 0xBB, 0xCC}
	sender.FeedCaptured(au)

	sRelay.mu.Lock()
	pkts := append([][]byte(nil), sRelay.sent...)
	sRelay.mu.Unlock()
	if len(pkts) == 0 {
		t.Fatal("sender produced no SRTP packets")
	}
	for _, p := range pkts {
		receiver.HandleRelayData(p)
	}
	if got == nil {
		t.Fatal("receiver emitted no frame")
	}
	if !bytes.Contains(got, []byte{0x67, 0xAA, 0xBB, 0xCC}) {
		t.Fatalf("reassembled frame missing NALU payload: %x", got)
	}
}
