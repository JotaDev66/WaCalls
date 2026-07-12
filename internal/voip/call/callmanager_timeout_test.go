package call

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"wacalls/internal/voip/core"
)

func watchdogCM(t Timeouts) *CallManager {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.timeouts = t
	m.watchdogTick = 5 * time.Millisecond
	return m
}

func TestWatchdogExpiresUnansweredOutgoing(t *testing.T) {
	m := watchdogCM(Timeouts{Answer: 30 * time.Millisecond})
	ended := make(chan *CallInfo, 1)
	m.OnEnded = func(c *CallInfo) { ended <- c }

	m.currentCall = NewOutgoingCall("c1", "peer@lid", "me@lid", core.CallMediaTypeAudio)
	if err := m.currentCall.ApplyTransition(Transition{Type: TransitionOfferSent}); err != nil {
		t.Fatalf("to ringing: %v", err)
	}
	m.startWatchdog()

	select {
	case c := <-ended:
		if c.StateData.EndReason != core.EndCallReasonTimeout {
			t.Fatalf("expected timeout reason, got %s", c.StateData.EndReason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unanswered outgoing call was not expired")
	}
}

func TestWatchdogExpiresStuckConnecting(t *testing.T) {
	m := watchdogCM(Timeouts{MediaConnect: 30 * time.Millisecond})
	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	if err := m.currentCall.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		t.Fatalf("to connecting: %v", err)
	}
	m.startWatchdog()

	waitFor(t, 2*time.Second, func() bool { return m.CurrentCall().IsEnded() })
	if r := m.CurrentCall().StateData.EndReason; r != core.EndCallReasonTimeout {
		t.Fatalf("expected timeout reason, got %s", r)
	}
}

func TestWatchdogDoesNotKillHealthyCall(t *testing.T) {
	m := watchdogCM(Timeouts{Ring: 20 * time.Millisecond, Answer: 20 * time.Millisecond})
	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	if err := m.currentCall.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		t.Fatalf("to connecting: %v", err)
	}
	if err := m.currentCall.ApplyTransition(Transition{Type: TransitionMediaConnected}); err != nil {
		t.Fatalf("to active: %v", err)
	}
	m.startWatchdog()

	time.Sleep(80 * time.Millisecond)
	if !m.CurrentCall().IsActive() {
		t.Fatalf("active call must survive ring/answer deadlines, got %s", m.CurrentCall().StateData.State)
	}
	_ = m.EndCall(context.Background(), core.EndCallReasonUserEnded)
}

func TestWatchdogCapsActiveCallDuration(t *testing.T) {
	m := watchdogCM(Timeouts{MaxDuration: 30 * time.Millisecond})
	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionLocalAccepted})
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionMediaConnected})
	m.startWatchdog()

	waitFor(t, 2*time.Second, func() bool { return m.CurrentCall().IsEnded() })
	if r := m.CurrentCall().StateData.EndReason; r != core.EndCallReasonTimeout {
		t.Fatalf("expected timeout reason, got %s", r)
	}
}

func TestWatchdogGoroutineStopsAfterEnd(t *testing.T) {
	obs := &countingObserver{}
	m := watchdogCM(Timeouts{Answer: 20 * time.Millisecond})
	m.observer = obs
	m.currentCall = NewOutgoingCall("c1", "peer@lid", "me@lid", core.CallMediaTypeAudio)
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionOfferSent})
	m.startWatchdog()

	waitFor(t, 2*time.Second, func() bool { return m.CurrentCall().IsEnded() })
	waitFor(t, 2*time.Second, func() bool { return obs.gorNow() == 0 })
}
