package core

import "testing"

func TestNopObserverSatisfiesPortAndDoesNotPanic(t *testing.T) {
	var obs CallObserver = NopObserver{}
	obs.Mark("x")
	obs.AddMem(10)
	obs.ReleaseMem(10)
	done := obs.TrackGoroutine()
	if done == nil {
		t.Fatal("TrackGoroutine must return a non-nil done func")
	}
	done()
	obs.End("ok", "")
}
