package app

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/core"
)

func ownerPtr(s string) *string { return &s }

func TestOwnerActiveCall(t *testing.T) {
	b := NewBroker(nil, slog.Default())
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

type fakeRecordStore struct {
	mu   sync.Mutex
	recs []core.CallRecord
}

func (f *fakeRecordStore) Insert(ctx context.Context, r core.CallRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs = append(f.recs, r)
	return nil
}

func (f *fakeRecordStore) List(ctx context.Context, sessionID string, limit int, before core.HistoryCursor) ([]core.CallRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []core.CallRecord{}
	for i := len(f.recs) - 1; i >= 0 && len(out) < limit; i-- {
		r := f.recs[i]
		if sessionID != "" && r.SessionID != sessionID {
			continue
		}
		if before != (core.HistoryCursor{}) && r.EndedAt >= before.EndedAt && (r.EndedAt != before.EndedAt || r.CallID >= before.CallID) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRecordStore) Prune(ctx context.Context, keep int) error { return nil }

func TestEndCallPersistsRecord(t *testing.T) {
	fake := &fakeRecordStore{}
	b := NewBroker(fake, slog.Default())
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Direction: "inbound", Peer: "p", StartedAt: 100, Status: StatusConnected})
	b.endCall("c1", "user_ended")

	recs, _ := fake.List(context.Background(), "s1", 10, core.HistoryCursor{})
	if len(recs) != 1 || recs[0].CallID != "c1" || recs[0].EndReason != "user_ended" || recs[0].EndedAt == 0 {
		t.Fatalf("ended call must be persisted, got %+v", recs)
	}

	rows, err := b.historyRows(context.Background(), "s1", 10)
	if err != nil || len(rows) != 1 || rows[0].Status != StatusEnded || rows[0].CallID != "c1" {
		t.Fatalf("history must read from the store, got %+v err %v", rows, err)
	}
}

func TestBroadcastKicksLaggingSubscriber(t *testing.T) {
	b := NewBroker(nil, slog.Default())
	sub := b.subscribe("slow")
	defer b.unsubscribe(sub)

	for i := range 33 {
		b.broadcast(map[string]any{"type": "call-list", "n": i})
	}

	select {
	case <-sub.kick:
	default:
		t.Fatal("lagging subscriber must be kicked after buffer overflow")
	}
}

func TestServeSSESendsSnapshotToNewSubscriber(t *testing.T) {
	b := NewBroker(nil, slog.Default())
	b.SnapshotFn = func() []any {
		return []any{map[string]any{"type": "session-list", "sessions": []SessionInfo{}}}
	}
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Status: StatusRinging})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	b.serveSSE(rec, r, "test-client")

	body := rec.Body.String()
	if !strings.Contains(body, `"session-list"`) {
		t.Fatalf("snapshot must include session-list, got %q", body)
	}
	if !strings.Contains(body, `"call-list"`) || !strings.Contains(body, `"c1"`) {
		t.Fatalf("snapshot must include the live call list, got %q", body)
	}
}

func TestNilRecordStoreIsSafe(t *testing.T) {
	b := NewBroker(nil, slog.Default())
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Status: StatusRinging})
	b.endCall("c1", "declined")
	rows, err := b.historyRows(context.Background(), "", 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("nil store must yield empty history without error, got %+v err %v", rows, err)
	}
}
