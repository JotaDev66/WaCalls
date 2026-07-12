package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 15*time.Second {
		t.Fatalf("ReadTimeout = %v, want 15s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Fatalf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %v, want 0 (SSE streams)", srv.WriteTimeout)
	}
}

func corsServer(origins ...string) *Server {
	set := map[string]struct{}{}
	for _, o := range origins {
		set[o] = struct{}{}
	}
	return &Server{allowedOrigins: set, authorize: bearerAuthorizer("secret")}
}

func TestWithCORSAllowedOrigin(t *testing.T) {
	s := corsServer("https://app.example.com")
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.Header.Set("Origin", "https://app.example.com")
	s.withCORS(http.NotFoundHandler()).ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO = %q, want echoed origin", got)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("Allow-Headers must include Authorization, got %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q, want Origin", rec.Header().Get("Vary"))
	}
}

func TestWithCORSDisallowedOrigin(t *testing.T) {
	s := corsServer("https://app.example.com")
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	s.withCORS(http.NotFoundHandler()).ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO = %q, want empty for disallowed origin", got)
	}
}

func TestWithCORSPreflightShortCircuitsBeforeAuth(t *testing.T) {
	s := corsServer("https://app.example.com")
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("preflight must not reach the inner handler")
	})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/api/sessions", nil)
	r.Header.Set("Origin", "https://app.example.com")
	s.withCORS(sentinel).ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
}
