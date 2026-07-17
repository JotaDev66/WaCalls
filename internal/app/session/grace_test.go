package session

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGraceKeeperFiresAfterWindow(t *testing.T) {
	var fired []string
	var mu sync.Mutex
	g := newGraceKeeper(10*time.Millisecond, func(callID string) {
		mu.Lock()
		fired = append(fired, callID)
		mu.Unlock()
	})
	g.arm("c1")

	deadline := time.After(time.Second)
	for {
		mu.Lock()
		n := len(fired)
		mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("grace timer never fired")
		case <-time.After(2 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if fired[0] != "c1" {
		t.Fatalf("want expiry for c1, got %v", fired)
	}
}

func TestGraceKeeperCancelPreventsExpiry(t *testing.T) {
	var fired atomic.Int32
	g := newGraceKeeper(20*time.Millisecond, func(string) { fired.Add(1) })
	g.arm("c1")
	g.cancel("c1")
	time.Sleep(60 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("cancel must prevent expiry, fired %d times", fired.Load())
	}
}

func TestGraceKeeperReArmReplaces(t *testing.T) {
	var fired atomic.Int32
	g := newGraceKeeper(30*time.Millisecond, func(string) { fired.Add(1) })
	g.arm("c1")
	time.Sleep(10 * time.Millisecond)
	g.arm("c1") // re-arm restarts the window
	time.Sleep(60 * time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("re-arm must yield exactly one expiry, got %d", fired.Load())
	}
}

func TestOnBridgeDetachedArmsForCurrentBridge(t *testing.T) {
	s := &Session{
		bridges: map[string]*Bridge{},
		log:     slog.Default(),
	}
	var expired atomic.Int32
	s.grace = newGraceKeeper(5*time.Millisecond, func(string) { expired.Add(1) })

	b := &Bridge{}
	s.bridges["c1"] = b
	s.onBridgeDetached("c1", b)

	time.Sleep(40 * time.Millisecond)
	if expired.Load() != 1 {
		t.Fatalf("detaching the current bridge must arm the grace timer, expired %d", expired.Load())
	}
}

func TestOnBridgeDetachedIgnoresSupersededBridge(t *testing.T) {
	s := &Session{
		bridges: map[string]*Bridge{},
		log:     slog.Default(),
	}
	var expired atomic.Int32
	s.grace = newGraceKeeper(5*time.Millisecond, func(string) { expired.Add(1) })

	old := &Bridge{}
	current := &Bridge{}
	s.bridges["c1"] = current // a re-attach already swapped in a new bridge
	s.onBridgeDetached("c1", old)

	time.Sleep(40 * time.Millisecond)
	if expired.Load() != 0 {
		t.Fatalf("a superseded bridge must not arm the grace timer, expired %d", expired.Load())
	}
}
