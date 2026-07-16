package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"wacalls/internal/voip/core"
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

func TestRoutesHealthzOpenWithToken(t *testing.T) {
	s := &Server{authorize: bearerAuthorizer("secret")}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz without token: want 200, got %d", rec.Code)
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

type fakeAuth struct {
	admin     *core.AdminCredential
	validHash string
}

func (f *fakeAuth) GetAdmin(context.Context) (core.AdminCredential, bool, error) {
	if f.admin == nil {
		return core.AdminCredential{}, false, nil
	}
	return *f.admin, true, nil
}
func (f *fakeAuth) CreateAdmin(context.Context, string, string) error  { return nil }
func (f *fakeAuth) SetAdminPassword(context.Context, string) error     { return nil }
func (f *fakeAuth) CreateSession(context.Context, string, int64) error { return nil }
func (f *fakeAuth) SessionValid(_ context.Context, h string, _ int64) (bool, error) {
	return h != "" && h == f.validHash, nil
}
func (f *fakeAuth) DeleteSession(context.Context, string) error        { return nil }
func (f *fakeAuth) DeleteSessionsExcept(context.Context, string) error { return nil }
func (f *fakeAuth) PurgeExpiredSessions(context.Context, int64) error  { return nil }

func TestAuthorizeRequestNoCredential(t *testing.T) {
	s := &Server{auth: &fakeAuth{}, apiToken: ""}
	if s.authorizeRequest(httptest.NewRequest("GET", "/api/x", nil)) {
		t.Fatal("no cookie and no token must be rejected (no open mode)")
	}
}

func TestAuthorizeRequestToken(t *testing.T) {
	s := &Server{auth: &fakeAuth{}, apiToken: "secret"}
	r := httptest.NewRequest("GET", "/api/x", nil)
	if s.authorizeRequest(r) {
		t.Fatal("no bearer rejected")
	}
	r.Header.Set("Authorization", "Bearer secret")
	if !s.authorizeRequest(r) {
		t.Fatal("correct bearer allowed")
	}
}

func TestAuthorizeRequestCookie(t *testing.T) {
	valid := hashToken("cookie-token")
	s := &Server{auth: &fakeAuth{validHash: valid}, apiToken: ""}
	if s.authorizeRequest(httptest.NewRequest("GET", "/api/x", nil)) {
		t.Fatal("no cookie must be rejected")
	}
	r := httptest.NewRequest("GET", "/api/x", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "cookie-token"})
	if !s.authorizeRequest(r) {
		t.Fatal("valid session cookie allowed")
	}
	r2 := httptest.NewRequest("GET", "/api/x", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookie, Value: "wrong"})
	if s.authorizeRequest(r2) {
		t.Fatal("bad cookie rejected")
	}
}
