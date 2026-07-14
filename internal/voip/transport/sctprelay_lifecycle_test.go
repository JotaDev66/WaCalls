package transport

import (
	"sync"
	"testing"

	"wacalls/internal/voip/core"
)

type memObserver struct {
	mu       sync.Mutex
	released int64
}

func (o *memObserver) Mark(string)                  {}
func (o *memObserver) SrtpRecvDrop(string)          {}
func (o *memObserver) NoteQuality(core.CallQuality) {}
func (o *memObserver) AddMem(int64)                 {}
func (o *memObserver) ReleaseMem(b int64)           { o.mu.Lock(); o.released += b; o.mu.Unlock() }
func (o *memObserver) TrackGoroutine() func()       { return func() {} }
func (o *memObserver) End(string, string)           {}
func (o *memObserver) rel() int64                   { o.mu.Lock(); defer o.mu.Unlock(); return o.released }

func addFakeConn(m *SctpRelayManager, id string, mem int64) *relayConnection {
	conn := &relayConnection{id: id, stopCh: make(chan struct{}), mem: mem}
	conn.setState(relayStateOpen)
	m.mu.Lock()
	m.connections[id] = conn
	m.mu.Unlock()
	return conn
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestCleanupTearsDownAllAndResets(t *testing.T) {
	m := NewSctpRelayManager(nil)
	c1 := addFakeConn(m, "1.1.1.1:3480", 1024)
	c2 := addFakeConn(m, "2.2.2.2:3480", 2048)
	m.SetSsrc(42)
	m.SetSubscriptionSsrc(99)

	m.Cleanup()

	if m.HasConnection() || m.ConnectedCount() != 0 {
		t.Fatalf("Cleanup must empty connections (has=%v count=%d)", m.HasConnection(), m.ConnectedCount())
	}
	if !isClosed(c1.stopCh) || !isClosed(c2.stopCh) {
		t.Fatal("Cleanup must close every connection stopCh")
	}
	if _, ok := m.obs().(core.NopObserver); !ok {
		t.Fatalf("Cleanup must reset the observer to NopObserver, got %T", m.obs())
	}
}

func TestTeardownIdempotentReleasesMemOnce(t *testing.T) {
	m := NewSctpRelayManager(nil)
	obs := &memObserver{}
	m.SetObserver(obs)
	conn := &relayConnection{id: "1.1.1.1:3480", stopCh: make(chan struct{}), mem: 1024}

	m.teardown(conn)
	m.teardown(conn)

	if obs.rel() != 1024 {
		t.Fatalf("teardownOnce must release mem exactly once, released=%d", obs.rel())
	}
	if !isClosed(conn.stopCh) {
		t.Fatal("teardown must close stopCh")
	}
}
