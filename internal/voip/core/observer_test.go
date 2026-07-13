package core

import "testing"

func TestNopObserverSatisfiesPortAndDoesNotPanic(t *testing.T) {
	var obs CallObserver = NopObserver{}
	obs.Mark("x")
	obs.SrtpRecvDrop("replay")
	obs.AddMem(10)
	obs.ReleaseMem(10)
	done := obs.TrackGoroutine()
	if done == nil {
		t.Fatal("TrackGoroutine must return a non-nil done func")
	}
	done()
	obs.End("ok", "")
}

type recObserver struct {
	marks []string
	mem   []int64
	gor   int
	ends  []string
	drops []string
}

func (r *recObserver) Mark(e string)      { r.marks = append(r.marks, e) }
func (r *recObserver) AddMem(b int64)     { r.mem = append(r.mem, b) }
func (r *recObserver) ReleaseMem(b int64) { r.mem = append(r.mem, -b) }
func (r *recObserver) TrackGoroutine() func() {
	r.gor++
	return func() { r.gor-- }
}
func (r *recObserver) End(result, reason string)  { r.ends = append(r.ends, result+"/"+reason) }
func (r *recObserver) SrtpRecvDrop(reason string) { r.drops = append(r.drops, reason) }

func TestMultiObserverFansOut(t *testing.T) {
	a, b := &recObserver{}, &recObserver{}
	m := MultiObserver(a, b)
	m.Mark(MarkTransportICE)
	m.SrtpRecvDrop("auth_failed")
	m.AddMem(64)
	m.ReleaseMem(64)
	done := m.TrackGoroutine()
	if a.gor != 1 || b.gor != 1 {
		t.Fatalf("TrackGoroutine must reach both, got %d/%d", a.gor, b.gor)
	}
	done()
	if a.gor != 0 || b.gor != 0 {
		t.Fatalf("done must decrement both, got %d/%d", a.gor, b.gor)
	}
	m.End("completed", "user_ended")
	for _, r := range []*recObserver{a, b} {
		if len(r.marks) != 1 || r.marks[0] != "transport.ice" {
			t.Fatalf("marks = %v", r.marks)
		}
		if len(r.mem) != 2 || r.mem[0] != 64 || r.mem[1] != -64 {
			t.Fatalf("mem = %v", r.mem)
		}
		if len(r.ends) != 1 || r.ends[0] != "completed/user_ended" {
			t.Fatalf("ends = %v", r.ends)
		}
		if len(r.drops) != 1 || r.drops[0] != "auth_failed" {
			t.Fatalf("drops = %v", r.drops)
		}
	}
}

func TestMultiObserverDegenerate(t *testing.T) {
	if _, ok := MultiObserver().(NopObserver); !ok {
		t.Fatal("zero observers must collapse to NopObserver")
	}
	a := &recObserver{}
	if got := MultiObserver(a); got != CallObserver(a) {
		t.Fatal("single observer must be returned as-is")
	}
}
