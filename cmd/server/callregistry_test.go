package main

import "testing"

func TestCallRegistryAddGetRemove(t *testing.T) {
	r := newCallRegistry()
	if r.count() != 0 {
		t.Fatalf("new registry must be empty, got %d", r.count())
	}

	a := &activeCall{}
	r.add("c1", a)
	r.add("c2", &activeCall{})
	if r.count() != 2 {
		t.Fatalf("expected 2, got %d", r.count())
	}

	got, ok := r.get("c1")
	if !ok || got != a {
		t.Fatalf("get c1 mismatch: ok=%v", ok)
	}

	removed, ok := r.remove("c1")
	if !ok || removed != a {
		t.Fatalf("remove c1 should return it: ok=%v", ok)
	}
	if _, ok := r.get("c1"); ok {
		t.Fatal("c1 must be gone after remove")
	}
	if _, ok := r.remove("c1"); ok {
		t.Fatal("removing twice must report not-found (idempotent)")
	}
	if r.count() != 1 {
		t.Fatalf("expected 1 left, got %d", r.count())
	}
}

func TestCallRegistryDrain(t *testing.T) {
	r := newCallRegistry()
	r.add("c1", &activeCall{})
	r.add("c2", &activeCall{})
	all := r.drain()
	if len(all) != 2 {
		t.Fatalf("drain should return 2, got %d", len(all))
	}
	if r.count() != 0 {
		t.Fatalf("registry must be empty after drain, got %d", r.count())
	}
}

func TestCallRegistrySetBridgeMissing(t *testing.T) {
	r := newCallRegistry()
	_, found := r.setBridge("nope", nil)
	if found {
		t.Fatal("setBridge on missing call must report not-found")
	}
}

func TestCallRegistryBridgeConcurrentAccess(t *testing.T) {
	r := newCallRegistry()
	r.add("c1", &activeCall{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			r.setBridge("c1", &Bridge{})
		}
	}()
	for i := 0; i < 100; i++ {
		r.bridge("c1")
	}
	<-done
}

func TestCallRegistryBridgeMissing(t *testing.T) {
	r := newCallRegistry()
	r.add("c1", &activeCall{})
	if _, ok := r.bridge("c1"); ok {
		t.Fatal("bridge should report not-found when no bridge is set yet")
	}
	b := &Bridge{}
	r.setBridge("c1", b)
	got, ok := r.bridge("c1")
	if !ok || got != b {
		t.Fatalf("bridge should return the set bridge: ok=%v got=%v", ok, got)
	}
}
