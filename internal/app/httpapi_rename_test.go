package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"wacalls/internal/app/events"
	"wacalls/internal/app/session"
	"wacalls/internal/store/sqlite"

	"go.mau.fi/whatsmeow"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func newRenameTestServer(t *testing.T) *Server {
	t.Helper()
	ctx := context.Background()
	bundle, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bundle.Close() })
	broker := events.NewBroker(bundle.Calls, slog.Default())
	mgr := session.NewManager(session.Deps{
		Ctx: ctx, Container: bundle.Container, Broker: broker,
		Store: bundle.Sessions, WALogger: waLog.Noop, Log: slog.Default(), Photos: bundle.Photos,
	})
	if err := bundle.Sessions.Insert(ctx, "s1", "Account A"); err != nil {
		t.Fatal(err)
	}
	mgr.NewSession("s1", "Account A", whatsmeow.NewClient(bundle.Container.NewDevice(), waLog.Noop))
	return &Server{authorize: bearerAuthorizer("secret"), broker: broker, sessions: mgr}
}

func TestSessionRename(t *testing.T) {
	s := newRenameTestServer(t)

	do := func(sid, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PATCH", "/api/sessions/"+sid, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		return rec
	}

	if rec := do("s1", `{"name":"Sales"}`, "secret"); rec.Code != 204 {
		t.Fatalf("want 204, got %d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	var body struct {
		Sessions []struct {
			Name string `json:"name"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sessions) != 1 || body.Sessions[0].Name != "Sales" {
		t.Fatalf("rename not visible in list: %s", rec.Body.String())
	}

	if rec := do("s1", `{"name":"  "}`, "secret"); rec.Code != 400 {
		t.Fatalf("empty name: want 400, got %d", rec.Code)
	}
	if rec := do("missing", `{"name":"X"}`, "secret"); rec.Code != 404 {
		t.Fatalf("unknown sid: want 404, got %d", rec.Code)
	}
	if rec := do("s1", `{"name":"X"}`, ""); rec.Code != 401 {
		t.Fatalf("no token: want 401, got %d", rec.Code)
	}
}
