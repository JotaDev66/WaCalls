package app

import (
	"testing"

	"wacalls/internal/voip/core"
)

func TestMapStatusReconnecting(t *testing.T) {
	if got := mapStatus(core.CallStateReconnecting); got != StatusReconnecting {
		t.Fatalf("expected reconnecting, got %s", got)
	}
	if got := mapStatus(core.CallStateActive); got != StatusConnected {
		t.Fatalf("active must stay connected, got %s", got)
	}
}
