package app

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"wacalls/internal/app/events"
	"wacalls/internal/voip/call"
)

func emptyCallSession(id string) *Session {
	return &Session{id: id, log: slog.Default(), calls: call.NewClient(nil, slog.Default(), nil, 0, nil, nil)}
}

func TestAcceptUnknownCallIs404(t *testing.T) {
	sess := emptyCallSession("s1")
	s := &Server{
		authorize: bearerAuthorizer(""),
		broker:    events.NewBroker(nil, slog.Default()),
		sessions:  &SessionManager{sessions: map[string]*Session{"s1": sess}},
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("POST", "/api/sessions/s1/calls/ghost/accept", nil))
	if rec.Code != 404 {
		t.Fatalf("accept unknown call: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}
