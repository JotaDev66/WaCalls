package transport

import (
	"sync"
	"testing"

	"github.com/pion/webrtc/v4"
)

type usableRecorder struct {
	mu   sync.Mutex
	vals []int
}

func (r *usableRecorder) record(u int) { r.mu.Lock(); r.vals = append(r.vals, u); r.mu.Unlock() }
func (r *usableRecorder) last() (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.vals) == 0 {
		return 0, false
	}
	return r.vals[len(r.vals)-1], true
}
func (r *usableRecorder) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.vals) }

func TestIceDisconnectedKeepsConnectionAndReportsUnusable(t *testing.T) {
	m := NewSctpRelayManager(nil)
	rec := &usableRecorder{}
	m.SetOnUsableChange(rec.record)
	conn := addFakeConn(m, "1.1.1.1:3480", 0)
	m.recomputeHealth()

	m.handleICEState(conn, webrtc.ICEConnectionStateDisconnected)

	if !m.HasConnection() {
		t.Fatal("disconnected conn must stay in the map (transient)")
	}
	if !conn.degraded.Load() {
		t.Fatal("disconnected conn must be marked degraded")
	}
	if last, ok := rec.last(); !ok || last != 0 {
		t.Fatalf("expected usable=0 reported, got %v %v", last, ok)
	}
	if isClosed(conn.stopCh) {
		t.Fatal("disconnected must not tear the connection down")
	}
}

func TestIceRecoveryClearsDegradedAndReportsUsable(t *testing.T) {
	m := NewSctpRelayManager(nil)
	rec := &usableRecorder{}
	m.SetOnUsableChange(rec.record)
	conn := addFakeConn(m, "1.1.1.1:3480", 0)
	m.recomputeHealth()
	m.handleICEState(conn, webrtc.ICEConnectionStateDisconnected)

	m.handleICEState(conn, webrtc.ICEConnectionStateConnected)

	if conn.degraded.Load() {
		t.Fatal("recovery must clear degraded")
	}
	if last, ok := rec.last(); !ok || last != 1 {
		t.Fatalf("expected usable=1 reported, got %v %v", last, ok)
	}
}

func TestIceFailedStillTearsDown(t *testing.T) {
	m := NewSctpRelayManager(nil)
	rec := &usableRecorder{}
	m.SetOnUsableChange(rec.record)
	conn := addFakeConn(m, "1.1.1.1:3480", 0)
	m.recomputeHealth()

	m.handleICEState(conn, webrtc.ICEConnectionStateFailed)

	if m.HasConnection() {
		t.Fatal("failed conn must be removed")
	}
	if !isClosed(conn.stopCh) {
		t.Fatal("failed conn must be torn down")
	}
	if last, ok := rec.last(); !ok || last != 0 {
		t.Fatalf("expected usable=0 reported, got %v %v", last, ok)
	}
}

func TestCleanupResetsUsableWithoutCallback(t *testing.T) {
	m := NewSctpRelayManager(nil)
	rec := &usableRecorder{}
	m.SetOnUsableChange(rec.record)
	addFakeConn(m, "1.1.1.1:3480", 0)
	m.recomputeHealth()
	before := rec.count()

	m.Cleanup()

	if rec.count() != before {
		t.Fatal("Cleanup must not fire onUsableChange")
	}
	m.mu.Lock()
	lu := m.lastUsable
	m.mu.Unlock()
	if lu != 0 {
		t.Fatalf("Cleanup must reset lastUsable, got %d", lu)
	}
}
