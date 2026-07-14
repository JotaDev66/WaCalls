package call

import (
	"testing"
	"time"
)

func TestMarkTapFiresOnMark(t *testing.T) {
	cm := &CallManager{}
	type capture struct {
		callID, mark string
		elapsed      int64
	}
	got := make(chan capture, 1)
	cm.OnMark = func(callID, mark string, elapsedMs int64) {
		got <- capture{callID, mark, elapsedMs}
	}

	tap := markTap{cm: cm, callID: "c1", start: time.Now().Add(-50 * time.Millisecond)}
	tap.Mark("transport.ice")

	select {
	case c := <-got:
		if c.callID != "c1" || c.mark != "transport.ice" {
			t.Fatalf("OnMark got %+v", c)
		}
		if c.elapsed < 40 {
			t.Fatalf("elapsed = %d, want >= ~50", c.elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("OnMark not fired")
	}
}

func TestMarkTapNilOnMarkIsSafe(t *testing.T) {
	tap := markTap{cm: &CallManager{}, callID: "c1", start: time.Now()}
	tap.Mark("transport.ice") // nil OnMark must not panic
}
