package call

import (
	"log/slog"
	"sync"
	"testing"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

type dropRecordingObserver struct {
	core.NopObserver
	mu    sync.Mutex
	drops []string
}

func (o *dropRecordingObserver) SrtpRecvDrop(reason string) {
	o.mu.Lock()
	o.drops = append(o.drops, reason)
	o.mu.Unlock()
}

func (o *dropRecordingObserver) recorded() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.drops...)
}

func dropTestPair(t *testing.T, obs core.CallObserver) (*CallManager, *CallManager, *[][]byte) {
	t.Helper()
	k1, k2 := km(1), km(9)

	recv := NewCallManager(fakeSock{}, slog.Default())
	recv.relay = &fakeRelay{}
	recv.srtp = engine.NewSrtpManager(k2, k1, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	recv.selfSsrc = 2000
	recv.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	recv.observer = obs
	recv.registerRTPHandler(core.PayloadTypeWhatsAppOpus, func(*media.RtpPacket) {})

	var pkts [][]byte
	send := NewCallManager(fakeSock{}, slog.Default())
	send.relay = &fakeRelay{onData: func(b []byte) { pkts = append(pkts, append([]byte(nil), b...)) }}
	send.srtp = engine.NewSrtpManager(k1, k2, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	send.rtpSession = media.NewWhatsAppOpusSession(1000)
	send.selfSsrc = 1000
	return send, recv, &pkts
}

func TestReplayedInboundPacketReachesObserverAndResetsOnCleanup(t *testing.T) {
	obs := &dropRecordingObserver{}
	send, recv, pkts := dropTestPair(t, obs)

	if err := send.sendAudioFrame([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 320); err != nil {
		t.Fatalf("sendAudioFrame: %v", err)
	}
	if len(*pkts) != 1 {
		t.Fatalf("expected 1 captured packet, got %d", len(*pkts))
	}
	wire := (*pkts)[0]

	recv.onRelayData(wire)
	if got := obs.recorded(); len(got) != 0 {
		t.Fatalf("fresh packet must not count a drop, got %v", got)
	}

	recv.onRelayData(wire)
	if got := obs.recorded(); len(got) != 1 || got[0] != "replay" {
		t.Fatalf("drops = %v, want [replay]", got)
	}

	recv.cleanupMedia()
	if left := recv.srtpDrops.snapshotAndReset(); len(left) != 0 {
		t.Fatalf("tally must be empty after cleanupMedia, got %v", left)
	}
}

func TestCorruptedAuthTagCountsAuthFailedDrop(t *testing.T) {
	obs := &dropRecordingObserver{}
	send, recv, pkts := dropTestPair(t, obs)
	defer recv.cleanupMedia()

	if err := send.sendAudioFrame([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 320); err != nil {
		t.Fatalf("sendAudioFrame: %v", err)
	}
	wire := append([]byte(nil), (*pkts)[0]...)
	wire[len(wire)-1] ^= 0xFF

	recv.onRelayData(wire)
	if got := obs.recorded(); len(got) != 1 || got[0] != "auth_failed" {
		t.Fatalf("drops = %v, want [auth_failed]", got)
	}
}

func TestSrtpDropTallyFirstOccurrenceAndReset(t *testing.T) {
	var tally srtpDropTally
	if !tally.add("replay") {
		t.Fatal("first replay must report first occurrence")
	}
	if tally.add("replay") {
		t.Fatal("second replay must not report first occurrence")
	}
	if !tally.add("auth_failed") {
		t.Fatal("first auth_failed must report first occurrence")
	}
	got := tally.snapshotAndReset()
	if got["replay"] != 2 || got["auth_failed"] != 1 {
		t.Fatalf("snapshot = %v", got)
	}
	if left := tally.snapshotAndReset(); len(left) != 0 {
		t.Fatalf("second snapshot must be empty, got %v", left)
	}
	if !tally.add("replay") {
		t.Fatal("after reset, replay must be first occurrence again")
	}
}
