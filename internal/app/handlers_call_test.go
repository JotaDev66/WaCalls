package app

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"wacalls/internal/app/events"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

func callServerWithEmptySession(id string) *Server {
	b := events.NewBroker(nil, slog.Default())
	mgr := NewManager(Deps{Broker: b, Log: slog.Default()})
	mgr.NewSession(id, "", &whatsmeow.Client{Store: &store.Device{}})
	return &Server{authorize: bearerAuthorizer(""), broker: b, sessions: mgr}
}

func TestAcceptUnknownCallIs404(t *testing.T) {
	s := callServerWithEmptySession("s1")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("POST", "/api/sessions/s1/calls/ghost/accept", nil))
	if rec.Code != 404 {
		t.Fatalf("accept unknown call: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestWebRTCUnknownCallIs404(t *testing.T) {
	s := callServerWithEmptySession("s1")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("POST", "/api/sessions/s1/calls/ghost/webrtc",
		strings.NewReader(`{"sdp_offer":"x"}`)))
	if rec.Code != 404 {
		t.Fatalf("webrtc unknown call: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}
