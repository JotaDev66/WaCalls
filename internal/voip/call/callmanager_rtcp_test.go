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

func mid32For(nowMs uint64) uint32 {
	ntpSec := uint32(nowMs/1000 + 2208988800)
	ntpFrac := uint32(float64(nowMs%1000) / 1000.0 * 4294967296.0)
	return ntpSec<<16 | ntpFrac>>16
}

func TestOnRelayDataDecodesInboundRtcp(t *testing.T) {
	obs := &countingObserver{}
	m := activeRtcpManager(t, nil)
	m.observer = obs

	recv, err := media.NewSrtcpContext(km(3))
	if err != nil {
		t.Fatalf("srtcp recv context: %v", err)
	}
	m.recvSrtcp = recv

	// Anchor LSR to the wall clock onRelayData reads internally (time.Now); "1 s ago" leaves ample
	// margin over the few-ms gap so the RTT underflow guard does not reject a legitimate sample.
	now := uint64(time.Now().UnixMilli())
	rb := &media.RTCPReportBlock{SSRC: m.selfSsrc, LSR: mid32For(now) - (1 << 16), DLSR: 0}
	sr := media.BuildRTCPCompound(m.peerSsrcs[0], media.RTCPSenderStats{}, rb, "peer@wacalls", now)
	protected, err := recv.Protect(sr, 0)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}

	m.onRelayData(protected)

	if obs.qualityNow() == 0 {
		t.Fatal("onRelayData did not emit NoteQuality for a valid inbound rtcp compound")
	}
	if !m.recvStats.QualitySnapshot(now).HasRtt {
		t.Fatal("peer report block echoing our ssrc did not yield rtt")
	}
}

func TestOnRelayDataDropsUnauthenticatedRtcp(t *testing.T) {
	obs := &dropRecordingObserver{}
	m := activeRtcpManager(t, nil)
	m.observer = obs

	recv, err := media.NewSrtcpContext(km(3))
	if err != nil {
		t.Fatalf("srtcp recv context: %v", err)
	}
	m.recvSrtcp = recv

	sr := media.BuildRTCPCompound(m.peerSsrcs[0], media.RTCPSenderStats{}, nil, "peer@wacalls", 1000)
	protected, err := recv.Protect(sr, 0)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	protected[len(protected)-1] ^= 0xff // corrupt the auth tag

	m.onRelayData(protected)

	drops := m.srtcpDrops.snapshotAndReset()
	if len(drops) != 1 || drops[string(media.SrtpErrAuthFailed)] != 1 {
		t.Fatalf("expected one auth_failed srtcp drop, got %v", drops)
	}
	// An SRTCP decode failure must not reach the media-plane SrtpRecvDrop counter.
	if got := obs.recorded(); len(got) != 0 {
		t.Fatalf("srtcp drop leaked into media SrtpRecvDrop: %v", got)
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
