package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"wacalls/internal/app/events"
	"wacalls/internal/store"
	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
)

func historyServer(t *testing.T) (*Server, core.CallRecordStore) {
	t.Helper()
	bundle, err := store.Open(context.Background(), store.Config{SQLitePath: filepath.Join(t.TempDir(), "history.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bundle.Close() })
	b := events.NewBroker(bundle.Calls, slog.Default())
	mgr := NewManager(Deps{Broker: b, Log: slog.Default()})
	mgr.NewSession("s1", "", &whatsmeow.Client{Store: &wastore.Device{}})
	s := &Server{
		authorize: bearerAuthorizer(""),
		broker:    b,
		sessions:  mgr,
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
	Calls      []events.CallRecord `json:"calls"`
	NextCursor string              `json:"nextCursor"`
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

func TestHistoryExportCSV(t *testing.T) {
	s, st := historyServer(t)
	owner := "op-A"
	recs := []core.CallRecord{
		{CallID: "c1", SessionID: "s1", Owner: &owner, Direction: "outbound", Peer: `1 "quoted", comma@x`, StartedAt: 1700000000000, EndedAt: 1700000060000, EndReason: "user_ended"},
		{CallID: "c2", SessionID: "s1", Direction: "inbound", Peer: "222@s.whatsapp.net", StartedAt: 1700000100000, EndedAt: 1700000160000, EndReason: "timeout"},
	}
	for _, r := range recs {
		if err := st.Insert(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history/export", nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type: %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="history-s1.csv"` {
		t.Fatalf("content-disposition: %q", cd)
	}
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"callId", "direction", "peer", "owner", "startedAt", "endedAt", "endReason"},
		{"c2", "inbound", "222@s.whatsapp.net", "", "2023-11-14T22:15:00Z", "2023-11-14T22:16:00Z", "timeout"},
		{"c1", "outbound", `1 "quoted", comma@x`, "op-A", "2023-11-14T22:13:20Z", "2023-11-14T22:14:20Z", "user_ended"},
	}
	if len(rows) != len(want) {
		t.Fatalf("want %d rows, got %d: %+v", len(want), len(rows), rows)
	}
	for i := range want {
		for j := range want[i] {
			if rows[i][j] != want[i][j] {
				t.Fatalf("row %d col %d: want %q, got %q", i, j, want[i][j], rows[i][j])
			}
		}
	}
}

func TestHistoryExportStreamsAllPages(t *testing.T) {
	s, st := historyServer(t)
	seedHistory(t, st, 510)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history/export", nil))
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil || len(rows) != 511 {
		t.Fatalf("want 511 csv rows, got %d err %v", len(rows), err)
	}
	seen := map[string]bool{}
	for _, r := range rows[1:] {
		if seen[r[0]] {
			t.Fatalf("duplicate %q across pages", r[0])
		}
		seen[r[0]] = true
	}
}

func TestHistoryExportEmptyAndUnknownSession(t *testing.T) {
	s, _ := historyServer(t)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/history/export", nil))
	if rec.Code != 200 || rec.Body.String() != "callId,direction,peer,owner,startedAt,endedAt,endReason\n" {
		t.Fatalf("empty export must be header only, got %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/ghost/history/export", nil))
	if rec.Code != 404 {
		t.Fatalf("want 404, got %d", rec.Code)
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
