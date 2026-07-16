package call

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/transport"
)

func activeCall() *CallInfo {
	c := NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	_ = c.ApplyTransition(Transition{Type: TransitionLocalAccepted})
	_ = c.ApplyTransition(Transition{Type: TransitionMediaConnected})
	return c
}

func TestMediaLostTransitionsActiveToReconnecting(t *testing.T) {
	c := activeCall()
	if err := c.ApplyTransition(Transition{Type: TransitionMediaLost}); err != nil {
		t.Fatalf("media_lost from active: %v", err)
	}
	if c.StateData.State != core.CallStateReconnecting {
		t.Fatalf("expected reconnecting, got %s", c.StateData.State)
	}
	if c.StateData.MediaLostAt == nil {
		t.Fatal("MediaLostAt must be set on media_lost")
	}
}

func TestMediaRestoredTransitionsReconnectingToActive(t *testing.T) {
	c := activeCall()
	_ = c.ApplyTransition(Transition{Type: TransitionMediaLost})
	if err := c.ApplyTransition(Transition{Type: TransitionMediaRestored}); err != nil {
		t.Fatalf("media_restored from reconnecting: %v", err)
	}
	if c.StateData.State != core.CallStateActive {
		t.Fatalf("expected active, got %s", c.StateData.State)
	}
	if c.StateData.MediaLostAt != nil {
		t.Fatal("MediaLostAt must be cleared on media_restored")
	}
}

func TestMediaLostInvalidOutsideActive(t *testing.T) {
	c := NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	if err := c.ApplyTransition(Transition{Type: TransitionMediaLost}); err == nil {
		t.Fatal("media_lost from incoming_ringing must be invalid")
	}
	_ = c.ApplyTransition(Transition{Type: TransitionLocalAccepted})
	if err := c.ApplyTransition(Transition{Type: TransitionMediaRestored}); err == nil {
		t.Fatal("media_restored from connecting must be invalid")
	}
}

func TestWatchdogExpiresReconnectingAfterGrace(t *testing.T) {
	m := watchdogCM(Timeouts{ReconnectGrace: 30 * time.Millisecond})
	ended := make(chan *CallInfo, 1)
	m.OnEnded = func(c *CallInfo) { ended <- c }
	m.currentCall = activeCall()
	if err := m.currentCall.ApplyTransition(Transition{Type: TransitionMediaLost}); err != nil {
		t.Fatalf("to reconnecting: %v", err)
	}
	m.startWatchdog()

	select {
	case c := <-ended:
		if c.StateData.EndReason != core.EndCallReasonTimeout {
			t.Fatalf("expected timeout reason, got %s", c.StateData.EndReason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnecting call was not expired after grace")
	}
}

func TestWatchdogSparesReconnectingWithinGrace(t *testing.T) {
	m := watchdogCM(Timeouts{ReconnectGrace: 10 * time.Second})
	m.currentCall = activeCall()
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionMediaLost})
	m.startWatchdog()

	time.Sleep(80 * time.Millisecond)
	if s, _ := stateOf(m); s != core.CallStateReconnecting {
		t.Fatalf("reconnecting call must survive within grace, got %s", s)
	}
	_ = m.EndCall(context.Background(), core.EndCallReasonUserEnded)
}

func TestPhaseDeadlineReconnectingDisabled(t *testing.T) {
	c := activeCall()
	_ = c.ApplyTransition(Transition{Type: TransitionMediaLost})
	if _, ok := phaseDeadline(c, Timeouts{}); ok {
		t.Fatal("zero ReconnectGrace must disable the deadline")
	}
}

func TestTerminateDuringReconnectingKeepsDuration(t *testing.T) {
	c := activeCall()
	past := time.Now().Add(-90 * time.Second)
	c.StateData.ConnectedAt = &past
	_ = c.ApplyTransition(Transition{Type: TransitionMediaLost})
	if err := c.ApplyTransition(Transition{Type: TransitionTerminated, Reason: core.EndCallReasonTimeout}); err != nil {
		t.Fatalf("terminate from reconnecting: %v", err)
	}
	if c.StateData.DurationSecs < 89 {
		t.Fatalf("terminate during reconnecting must keep duration, got %d", c.StateData.DurationSecs)
	}
}

func TestUsableZeroMovesActiveToReconnectingAndRedials(t *testing.T) {
	configured := make(chan int, 1)
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{onConfigure: func(r []transport.RelayConfig) { configured <- len(r) }}
	m.currentCall = activeCall()
	m.currentCall.RelayData = &core.RelayData{Endpoints: []core.RelayEndpoint{{
		IP: "9.9.9.9", Port: 3480, Key: "k", RawToken: []byte{1}, Protocol: 0,
	}}}

	m.onRelayUsableChange(0)

	if s, _ := stateOf(m); s != core.CallStateReconnecting {
		t.Fatalf("expected reconnecting, got %s", s)
	}
	select {
	case n := <-configured:
		if n != 1 {
			t.Fatalf("expected 1 relay config redialed, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("redial was not attempted")
	}
}

func TestUsablePositiveKeepsReconnectingUntilMedia(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = activeCall()
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionMediaLost})

	m.onRelayUsableChange(1)

	if s, _ := stateOf(m); s != core.CallStateReconnecting {
		t.Fatalf("transport recovery alone must not leave reconnecting, got %s", s)
	}
}

func TestInboundMediaRestoresActive(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{}
	m.selfSsrc = 1000
	m.currentCall = activeCall()
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionMediaLost})

	pkt := make([]byte, 12)
	pkt[0] = 0x80
	pkt[1] = 120
	pkt[11] = 0xD0
	m.onRelayData(pkt)

	if s, _ := stateOf(m); s != core.CallStateActive {
		t.Fatalf("inbound peer media must restore active, got %s", s)
	}
	if m.currentCall.StateData.MediaLostAt != nil {
		t.Fatal("MediaLostAt must be cleared")
	}
}

func TestWatchdogRetriesRedialWhileReconnecting(t *testing.T) {
	dropped := make(chan struct{}, 1)
	configured := make(chan int, 1)
	m := watchdogCM(Timeouts{ReconnectGrace: 10 * time.Second})
	m.relay = &fakeRelay{
		onDrop: func() {
			select {
			case dropped <- struct{}{}:
			default:
			}
		},
		onConfigure: func(r []transport.RelayConfig) {
			select {
			case configured <- len(r):
			default:
			}
		},
	}
	m.currentCall = activeCall()
	m.currentCall.RelayData = &core.RelayData{Endpoints: []core.RelayEndpoint{{
		IP: "9.9.9.9", Port: 3480, Key: "k", RawToken: []byte{1}, Protocol: 0,
	}}}
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionMediaLost})
	m.lastRedialAt = time.Now().Add(-time.Minute)
	m.startWatchdog()

	select {
	case <-dropped:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnecting must periodically recycle relay connections")
	}
	select {
	case <-configured:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect retry must redial the stored endpoints")
	}
	_ = m.EndCall(context.Background(), core.EndCallReasonUserEnded)
}

func TestWatchdogRecyclesOnMediaInactivity(t *testing.T) {
	dropped := make(chan struct{}, 1)
	configured := make(chan int, 1)
	m := watchdogCM(Timeouts{MediaInactivity: 30 * time.Millisecond, ReconnectGrace: 10 * time.Second})
	m.relay = &fakeRelay{
		onDrop: func() {
			select {
			case dropped <- struct{}{}:
			default:
			}
		},
		onConfigure: func(r []transport.RelayConfig) {
			select {
			case configured <- len(r):
			default:
			}
		},
	}
	m.currentCall = activeCall()
	m.currentCall.RelayData = &core.RelayData{Endpoints: []core.RelayEndpoint{{
		IP: "9.9.9.9", Port: 3480, Key: "k", RawToken: []byte{1}, Protocol: 0,
	}}}
	m.lastMediaRecv.Store(time.Now().Add(-time.Second).UnixMilli())
	m.startWatchdog()

	waitFor(t, 2*time.Second, func() bool { s, _ := stateOf(m); return s == core.CallStateReconnecting })
	select {
	case <-dropped:
	case <-time.After(2 * time.Second):
		t.Fatal("stale media must drop the surviving relay connections")
	}
	select {
	case <-configured:
	case <-time.After(2 * time.Second):
		t.Fatal("recycle must redial the stored endpoints")
	}
	_ = m.EndCall(context.Background(), core.EndCallReasonUserEnded)
}

func TestMediaRestoredNotifiesPeerTransport(t *testing.T) {
	sock := &recordSock{}
	m := NewCallManager(sock, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = activeCall()
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionMediaLost})

	m.onRelayUsableChange(1)

	waitFor(t, 2*time.Second, func() bool {
		return slices.Contains(sock.sentInnerTags(), "transport")
	})
}

func TestUsableChangeNoopOutsideActiveOrReconnecting(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{}
	m.onRelayUsableChange(0)

	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionLocalAccepted})
	m.onRelayUsableChange(0)
	if s, _ := stateOf(m); s != core.CallStateConnecting {
		t.Fatalf("connecting call must not react to usable=0, got %s", s)
	}
	m.onRelayUsableChange(1)
	if s, _ := stateOf(m); s != core.CallStateConnecting {
		t.Fatalf("connecting call must not react to usable=1, got %s", s)
	}
}
