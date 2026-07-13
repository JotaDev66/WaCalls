package app

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func callsServer() *Server {
	b := NewBroker(nil, slog.Default())
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c-late", Direction: "outbound", Peer: "p1", StartedAt: 200, Status: StatusRinging})
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c-early", Direction: "inbound", Peer: "p2", StartedAt: 100, Status: StatusConnected})
	b.upsertCall(CallRecord{SessionID: "s2", CallID: "c-other", Direction: "inbound", Peer: "p3", StartedAt: 50, Status: StatusRinging})
	return &Server{
		authorize: bearerAuthorizer(""),
		broker:    b,
		sessions: &SessionManager{sessions: map[string]*Session{
			"s1": {id: "s1"},
			"s2": {id: "s2"},
		}},
	}
}

func TestCallListScopedAndSorted(t *testing.T) {
	s := callsServer()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/calls", nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Calls []CallRecord `json:"calls"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Calls) != 2 || body.Calls[0].CallID != "c-early" || body.Calls[1].CallID != "c-late" {
		t.Fatalf("want [c-early c-late] scoped to s1, got %+v", body.Calls)
	}
}

func TestCallListEmptyIsJSONArray(t *testing.T) {
	s := callsServer()
	s.broker.endCall("c-late", "declined")
	s.broker.endCall("c-early", "declined")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/calls", nil))
	if rec.Code != 200 || rec.Body.String() != "{\"calls\":[]}\n" {
		t.Fatalf("empty list must be a JSON array, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestCallListUnknownSession(t *testing.T) {
	s := callsServer()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/ghost/calls", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown session: want 404, got %d", rec.Code)
	}
}

func TestCallGetByID(t *testing.T) {
	s := callsServer()

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/calls/c-early", nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Call CallRecord `json:"call"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Call.CallID != "c-early" || body.Call.Status != StatusConnected {
		t.Fatalf("unexpected call payload: %+v", body.Call)
	}

	for _, path := range []string{
		"/api/sessions/s1/calls/ghost",
		"/api/sessions/s1/calls/c-other",
	} {
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 404 {
			t.Fatalf("%s: want 404, got %d", path, rec.Code)
		}
	}
}
