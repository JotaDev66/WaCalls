package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestTokenPrefersHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/x?access_token=q", nil)
	r.Header.Set("Authorization", "Bearer h")
	if got := requestToken(r); got != "h" {
		t.Fatalf("want header token h, got %q", got)
	}
}

func TestRequestTokenFallsBackToQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/x?access_token=q", nil)
	if got := requestToken(r); got != "q" {
		t.Fatalf("want query token q, got %q", got)
	}
}

func TestBearerAuthorizerEmptyTokenAllowsAll(t *testing.T) {
	authz := bearerAuthorizer("")
	if !authz(httptest.NewRequest("GET", "/api/x", nil)) {
		t.Fatal("empty token must allow all requests")
	}
}

func TestBearerAuthorizerChecksToken(t *testing.T) {
	authz := bearerAuthorizer("secret")
	r := httptest.NewRequest("GET", "/api/x", nil)
	if authz(r) {
		t.Fatal("no token must be rejected")
	}
	r.Header.Set("Authorization", "Bearer secret")
	if !authz(r) {
		t.Fatal("correct token must pass")
	}
	r.Header.Set("Authorization", "Bearer wrong")
	if authz(r) {
		t.Fatal("wrong token must be rejected")
	}
}

func TestWithAuthGates(t *testing.T) {
	s := &Server{authorize: bearerAuthorizer("secret")}
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := s.withAuth(sentinel)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: want 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/sessions", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: want 401, got %d", rec.Code)
	}
}

func TestWithAuthEmptyTokenPasses(t *testing.T) {
	s := &Server{authorize: bearerAuthorizer("")}
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	s.withAuth(sentinel).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty token: want 200, got %d", rec.Code)
	}
}

func TestRoutesUIOpenWithToken(t *testing.T) {
	s := &Server{authorize: bearerAuthorizer("secret")}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("UI shell must never be gated")
	}
}

func TestRoutesEventsRequireToken(t *testing.T) {
	s := &Server{authorize: bearerAuthorizer("secret")}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/events", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SSE without token: want 401, got %d", rec.Code)
	}
}
