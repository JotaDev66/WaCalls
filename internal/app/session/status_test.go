package session

import (
	"testing"

	"wacalls/internal/app/events"
	"wacalls/internal/voip/core"
)

func TestMapStatusReconnecting(t *testing.T) {
	if got := mapStatus(core.CallStateReconnecting); got != events.StatusReconnecting {
		t.Fatalf("expected reconnecting, got %s", got)
	}
	if got := mapStatus(core.CallStateActive); got != events.StatusConnected {
		t.Fatalf("active must stay connected, got %s", got)
	}
}
