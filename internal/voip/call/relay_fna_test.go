package call

import (
	"testing"

	"wacalls/internal/voip/core"
)

// An inbound call's uplink RTP from the peer lands on the relay the offer marked
// is_fna=1. With maxDialRelays capping the dialed set, the FNA endpoint must be
// moved to the front (keeping the RTT order among the rest) or the callee connects
// but never receives media.
func TestPrioritizeFNAMovesFNAFirstKeepingOrder(t *testing.T) {
	eps := []core.RelayEndpoint{
		{IP: "1.1.1.1", RelayName: "gru1"},
		{IP: "2.2.2.2", RelayName: "for2"},
		{IP: "3.3.3.3", RelayName: "frec43", IsFNA: true},
		{IP: "4.4.4.4", RelayName: "sfo1"},
	}
	got := prioritizeFNA(eps)
	want := []string{"frec43", "gru1", "for2", "sfo1"}
	for i, name := range want {
		if got[i].RelayName != name {
			t.Fatalf("order[%d] = %q, want %q (full: %v)", i, got[i].RelayName, name, got)
		}
	}
	if eps[0].RelayName != "gru1" {
		t.Fatal("prioritizeFNA must not mutate its input")
	}
}

func TestPrioritizeFNAWithoutFNAKeepsOrder(t *testing.T) {
	eps := []core.RelayEndpoint{
		{IP: "1.1.1.1", RelayName: "gru1"},
		{IP: "2.2.2.2", RelayName: "for2"},
	}
	got := prioritizeFNA(eps)
	if got[0].RelayName != "gru1" || got[1].RelayName != "for2" {
		t.Fatalf("order changed without FNA: %v", got)
	}
}
