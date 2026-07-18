package session

import (
	"testing"
	"time"
)

func TestIsStaleOffer(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name             string
		offerTS          time.Time
		offlineReplaying bool
		want             bool
	}{
		{"fresh live offer", now.Add(-1 * time.Second), false, false},
		{"old buffered offer", now.Add(-5 * time.Minute), false, true},
		{"just under threshold", now.Add(-(staleOfferThreshold - time.Second)), false, false},
		{"just over threshold", now.Add(-(staleOfferThreshold + time.Second)), false, true},
		{"zero timestamp, live", time.Time{}, false, false},
		{"zero timestamp during replay", time.Time{}, true, true},
		{"fresh timestamp during replay is still dropped", now.Add(-1 * time.Second), true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isStaleOffer(c.offerTS, c.offlineReplaying, now); got != c.want {
				t.Fatalf("isStaleOffer(%v, replaying=%v) = %v, want %v", c.offerTS, c.offlineReplaying, got, c.want)
			}
		})
	}
}
