package events

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	for range 200 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("file never appeared: %s", path)
}

func TestRecorderRoutesCallEventsPerCall(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewRecorder(dir, slog.Default())
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	rec.Offer(map[string]any{"type": "call-status", "id": "c1", "status": "connected"})
	rec.Offer(map[string]any{"type": "call-mark", "id": "c1", "mark": "transport.ice"})
	rec.Offer(map[string]any{"type": "call-status", "id": "c2", "status": "ringing"})
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	c1 := readJSONL(t, filepath.Join(dir, "call-c1.jsonl"))
	if len(c1) != 2 {
		t.Fatalf("want 2 lines for c1, got %d", len(c1))
	}
	if c1[0]["type"] != "call-status" || c1[1]["mark"] != "transport.ice" {
		t.Fatalf("bad c1 content: %+v", c1)
	}
	if _, ok := c1[0]["ts_ms"]; !ok {
		t.Fatal("recorder must inject ts_ms")
	}
	c2 := readJSONL(t, filepath.Join(dir, "call-c2.jsonl"))
	if len(c2) != 1 {
		t.Fatalf("want 1 line for c2, got %d", len(c2))
	}
}

func TestRecorderRoutesSessionEventsToSessionFile(t *testing.T) {
	dir := t.TempDir()
	rec, _ := NewRecorder(dir, slog.Default())
	rec.Offer(map[string]any{"type": "auth-state", "sessionId": "s1", "state": "open"})
	_ = rec.Close()

	lines := readJSONL(t, filepath.Join(dir, "session.jsonl"))
	if len(lines) != 1 || lines[0]["type"] != "auth-state" {
		t.Fatalf("session event must route to session.jsonl, got %+v", lines)
	}
}

func TestRecorderNilIsNoop(t *testing.T) {
	var rec *Recorder
	rec.Offer(map[string]any{"type": "call-status", "id": "c1"})
	if err := rec.Close(); err != nil {
		t.Fatalf("nil recorder close: %v", err)
	}
}

func TestBrokerBroadcastFeedsRecorder(t *testing.T) {
	dir := t.TempDir()
	rec, _ := NewRecorder(dir, slog.Default())
	b := NewBroker(nil, slog.Default())
	b.SetRecorder(rec)

	b.EmitCallPeerMute("s1", "c9", true)
	_ = rec.Close()

	waitForFile(t, filepath.Join(dir, "call-c9.jsonl"))
	lines := readJSONL(t, filepath.Join(dir, "call-c9.jsonl"))
	if len(lines) != 1 || lines[0]["type"] != "call-peer-mute" || lines[0]["muted"] != true {
		t.Fatalf("broadcast must feed the recorder, got %+v", lines)
	}
}
