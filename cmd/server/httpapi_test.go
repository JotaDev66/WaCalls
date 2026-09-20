package main

import "testing"

func TestCallOwnerAllowsUnclaimedCall(t *testing.T) {
	s := &server{broker: NewBroker()}
	s.broker.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Status: StatusRinging})

	if !s.callOwnerAllows("c1", "op-A") {
		t.Fatalf("unclaimed call should be allowed for op-A")
	}
	if !s.callOwnerAllows("c1", "op-B") {
		t.Fatalf("unclaimed call should be allowed for op-B")
	}
}

func TestCallOwnerAllowsOwnedCall(t *testing.T) {
	s := &server{broker: NewBroker()}
	s.broker.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Owner: ownerPtr("op-A"), Status: StatusConnected})

	if !s.callOwnerAllows("c1", "op-A") {
		t.Fatalf("owner op-A should be allowed to act on c1")
	}
	if s.callOwnerAllows("c1", "op-B") {
		t.Fatalf("non-owner op-B should not be allowed to act on c1")
	}
}

func TestCallOwnerAllowsUnknownCall(t *testing.T) {
	s := &server{broker: NewBroker()}

	if !s.callOwnerAllows("does-not-exist", "op-A") {
		t.Fatalf("unknown call id should be allowed (let the endpoint's 404 handle it)")
	}
}
