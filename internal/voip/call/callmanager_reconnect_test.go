package call

import (
	"testing"
	"time"

	"wacalls/internal/voip/core"
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
