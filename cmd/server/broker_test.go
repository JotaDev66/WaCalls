package main

import "testing"

func ownerPtr(s string) *string { return &s }

func TestOwnerActiveCall(t *testing.T) {
	b := NewBroker()
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Owner: ownerPtr("op-A"), Status: StatusConnected})
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c2", Owner: ownerPtr("op-B"), Status: StatusRinging})

	if got := b.ownerActiveCall("op-A"); got != "c1" {
		t.Fatalf("op-A should own c1, got %q", got)
	}
	if got := b.ownerActiveCall("op-C"); got != "" {
		t.Fatalf("op-C owns nothing, got %q", got)
	}
	if got := b.ownerActiveCall(""); got != "" {
		t.Fatalf("empty owner must return empty, got %q", got)
	}

	b.endCall("c1", "done")
	if got := b.ownerActiveCall("op-A"); got != "" {
		t.Fatalf("op-A's call ended, expected empty, got %q", got)
	}
}

func TestTryReserveOwnerIsExclusive(t *testing.T) {
	b := NewBroker()
	results := make(chan bool, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- b.tryReserveOwner("op-A")
		}()
	}
	close(start)
	a, c := <-results, <-results
	if a == c {
		t.Fatalf("exactly one reservation must succeed, got %v and %v", a, c)
	}
}

func TestTryReserveOwnerBlocksActiveCall(t *testing.T) {
	b := NewBroker()
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Owner: ownerPtr("op-A"), Status: StatusConnected})
	if b.tryReserveOwner("op-A") {
		t.Fatal("must not reserve for an owner with an already-active call")
	}
}

func TestReleaseReservationAllowsRetry(t *testing.T) {
	b := NewBroker()
	if !b.tryReserveOwner("op-A") {
		t.Fatal("first reservation should succeed")
	}
	if b.tryReserveOwner("op-A") {
		t.Fatal("second reservation while first is held must fail")
	}
	b.releaseReservation("op-A")
	if !b.tryReserveOwner("op-A") {
		t.Fatal("reservation should be available again after release")
	}
}
