package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"wacalls/internal/store"
	"wacalls/internal/voip/core"
)

func historyServer(t *testing.T) (*Server, core.CallRecordStore) {
	t.Helper()
	bundle, err := store.Open(context.Background(), store.Config{SQLitePath: filepath.Join(t.TempDir(), "history.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bundle.Close() })
	s := &Server{
		authorize: bearerAuthorizer(""),
		broker:    NewBroker(bundle.Calls, slog.Default()),
		sessions:  &SessionManager{sessions: map[string]*Session{"s1": {id: "s1"}}},
	}
	return s, bundle.Calls
}

func seedHistory(t *testing.T, st core.CallRecordStore, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		rec := core.CallRecord{
			CallID:    "c" + strconv.Itoa(i),
			SessionID: "s1",
			Direction: "outbound",
			Peer:      strconv.Itoa(i) + "@s.whatsapp.net",
			StartedAt: int64(i * 1000),
			EndedAt:   int64(i*1000 + 500),
			EndReason: "user_ended",
		}
		if err := st.Insert(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
}

type historyResp struct {
	Calls      []CallRecord `json:"calls"`
	NextCursor string       `json:"nextCursor"`
}

func TestHistoryPaginates(t *testing.T) {
	s, st := historyServer(t)
	seedHistory(t, st, 3)

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history?limit=2", nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var page1 historyResp
	if err := json.Unmarshal(rec.Body.Bytes(), &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Calls) != 2 || page1.Calls[0].CallID != "c3" || page1.Calls[1].CallID != "c2" {
		t.Fatalf("want [c3 c2], got %+v", page1.Calls)
	}
	if page1.NextCursor == "" {
		t.Fatal("want nextCursor on first page")
	}

	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history?limit=2&cursor="+page1.NextCursor, nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var page2 historyResp
	if err := json.Unmarshal(rec.Body.Bytes(), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Calls) != 1 || page2.Calls[0].CallID != "c1" || page2.NextCursor != "" {
		t.Fatalf("want [c1] no cursor, got %+v cursor %q", page2.Calls, page2.NextCursor)
	}
}

func TestHistoryDefaultLimit(t *testing.T) {
	s, st := historyServer(t)
	seedHistory(t, st, 3)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history", nil))
	var page historyResp
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(page.Calls) != 3 || page.NextCursor != "" {
		t.Fatalf("want all 3 without cursor, got %d %+v %q", rec.Code, page.Calls, page.NextCursor)
	}
}

func TestHistoryEmptyIsJSONArray(t *testing.T) {
	s, _ := historyServer(t)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history", nil))
	if rec.Code != 200 || rec.Body.String() != "{\"calls\":[]}\n" {
		t.Fatalf("empty history must be {\"calls\":[]}, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHistoryBadParams(t *testing.T) {
	s, _ := historyServer(t)
	for _, path := range []string{
		"/api/sessions/s1/history?limit=0",
		"/api/sessions/s1/history?limit=-5",
		"/api/sessions/s1/history?limit=abc",
		"/api/sessions/s1/history?cursor=!!!",
	} {
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 400 {
			t.Fatalf("%s: want 400, got %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/ghost/history", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown session: want 404, got %d", rec.Code)
	}
}

func TestHistoryLimitParsing(t *testing.T) {
	if n, err := historyLimit(""); err != nil || n != 50 {
		t.Fatalf("default: got %d %v", n, err)
	}
	if n, err := historyLimit("999"); err != nil || n != 200 {
		t.Fatalf("clamp: got %d %v", n, err)
	}
	for _, raw := range []string{"0", "-1", "abc", "1.5"} {
		if _, err := historyLimit(raw); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}

func TestHistoryCursorRoundTrip(t *testing.T) {
	c := core.HistoryCursor{EndedAt: 1700000060000, CallID: "call:with:colons"}
	got, err := decodeHistoryCursor(encodeHistoryCursor(c))
	if err != nil || got != c {
		t.Fatalf("round trip: got %+v err %v", got, err)
	}
	if z, err := decodeHistoryCursor(""); err != nil || z != (core.HistoryCursor{}) {
		t.Fatalf("empty must decode to zero, got %+v err %v", z, err)
	}
	for _, raw := range []string{"!!!", "bm9zZXBhcmF0b3I", "MTI6", "YWJjOng"} {
		if _, err := decodeHistoryCursor(raw); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}
