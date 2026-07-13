package call

import (
	"encoding/binary"
	"log/slog"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

func activeRtcpManager(t *testing.T, onSend func([]byte)) *CallManager {
	t.Helper()
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{onData: onSend}
	m.selfSsrc = 0x11223344
	m.peerSsrcs = []uint32{0x55667788}
	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	sc, err := media.NewSrtcpContext(km(3))
	if err != nil {
		t.Fatalf("srtcp context: %v", err)
	}
	m.sendSrtcp = sc
	m.recvStats = media.NewRTCPReceiverStats()
	m.rtcpCName = "test@wacalls"
	return m
}

func TestRtcpTxEmitsFramedSrtcp(t *testing.T) {
	var sent [][]byte
	m := activeRtcpManager(t, func(b []byte) { sent = append(sent, append([]byte(nil), b...)) })

	m.emitRtcp208()
	m.emitRtcpSR()
	m.emitRtcp209()

	if len(sent) != 3 {
		t.Fatalf("broadcast %d packets, want 3", len(sent))
	}

	wantPT := []byte{media.RTCPPayloadTypeCompact, media.RTCPPayloadTypeSR, media.RTCPPayloadTypeCompact2}
	for i, p := range sent {
		if len(p) < 8+4+10 {
			t.Fatalf("packet %d too short for srtcp framing: %d bytes", i, len(p))
		}
		if !transport.IsRtcpPacket(p) {
			t.Errorf("packet %d not classified as rtcp (byte0=%#x)", i, p[0])
		}
		if p[1] != wantPT[i] {
			t.Errorf("packet %d PT = %d, want %d", i, p[1], wantPT[i])
		}
		if ssrc := binary.BigEndian.Uint32(p[4:8]); ssrc != m.selfSsrc {
			t.Errorf("packet %d sender ssrc = %#x, want %#x", i, ssrc, m.selfSsrc)
		}
		// SRTCP trailer: 4-byte E-flag|index word followed by a 10-byte auth tag.
		word := binary.BigEndian.Uint32(p[len(p)-14 : len(p)-10])
		if word&0x80000000 == 0 {
			t.Errorf("packet %d E-bit not set", i)
		}
		if idx := word & 0x7fffffff; idx != uint32(i) {
			t.Errorf("packet %d srtcp index = %d, want %d", i, idx, i)
		}
	}
}

func TestRtcpTxNoopWhenNotReady(t *testing.T) {
	var sent int
	m := activeRtcpManager(t, func([]byte) { sent++ })
	m.sendSrtcp = nil // keying not yet derived

	m.emitRtcp208()
	m.emitRtcpSR()
	m.emitRtcp209()

	if sent != 0 {
		t.Fatalf("emitted %d packets before keying was ready, want 0", sent)
	}
}

func TestRtcpTxLoopBroadcasts(t *testing.T) {
	obs := &countingObserver{}
	var mu sync.Mutex
	var sent [][]byte
	m := activeRtcpManager(t, func(b []byte) {
		mu.Lock()
		sent = append(sent, append([]byte(nil), b...))
		mu.Unlock()
	})
	m.observer = obs
	m.rtcp208Tick = 5 * time.Millisecond
	m.rtcpSRTick = 7 * time.Millisecond
	m.rtcp209Tick = 11 * time.Millisecond

	m.mu.Lock()
	m.maybeStartRtcpTxLocked()
	m.mu.Unlock()

	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sent) > 0
	})

	m.cleanupMedia()
	waitFor(t, 2*time.Second, func() bool { return obs.gorNow() == 0 })

	mu.Lock()
	defer mu.Unlock()
	if len(sent) == 0 {
		t.Fatal("tx loop broadcast no rtcp packets")
	}
	for i, p := range sent {
		if !transport.IsRtcpPacket(p) {
			t.Errorf("loop packet %d not rtcp-framed (byte0=%#x)", i, p[0])
		}
	}
}

func TestRtcpTxLifecycle(t *testing.T) {
	obs := &countingObserver{}
	m := activeRtcpManager(t, nil)
	m.observer = obs

	m.mu.Lock()
	m.maybeStartRtcpTxLocked()
	m.maybeStartRtcpTxLocked() // idempotent: must not start a second loop
	m.mu.Unlock()

	if g := obs.gorNow(); g != 1 {
		t.Fatalf("tracked goroutines = %d, want 1", g)
	}

	m.cleanupMedia()
	waitFor(t, 2*time.Second, func() bool { return obs.gorNow() == 0 })

	m.mu.Lock()
	stopNil := m.rtcpTxStop == nil
	m.mu.Unlock()
	if !stopNil {
		t.Fatal("rtcpTxStop not cleared after cleanup")
	}
}
