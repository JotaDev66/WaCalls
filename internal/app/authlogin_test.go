package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"wacalls/internal/voip/core"

	"golang.org/x/crypto/bcrypt"
)

func loginServer(t *testing.T, user, pass string) *Server {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	fa := &fakeAuth{admin: &core.AdminCredential{Username: user, PasswordHash: string(hash)}}
	s := &Server{auth: fa, apiToken: ""}
	s.authorize = s.authorizeRequest
	return s
}

func TestAuthStatusAuthenticated(t *testing.T) {
	decode := func(rec *httptest.ResponseRecorder) bool {
		var body struct {
			Authenticated bool `json:"authenticated"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Authenticated
	}

	s := &Server{auth: &fakeAuth{}, apiToken: ""}
	rec := httptest.NewRecorder()
	s.handleAuthStatus(rec, httptest.NewRequest("GET", "/api/auth/status", nil))
	if decode(rec) {
		t.Fatal("no credential must report not authenticated")
	}

	s2 := &Server{auth: &fakeAuth{}, apiToken: "secret"}
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	s2.handleAuthStatus(rec2, req)
	if !decode(rec2) {
		t.Fatal("valid bearer must report authenticated")
	}
}

func TestLoginSuccessSetsCookie(t *testing.T) {
	s := loginServer(t, "root", "pw123456")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"root","password":"pw123456"}`))
	s.handleLogin(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), sessionCookie) {
		t.Fatalf("expected session cookie, got %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestLoginBadPassword(t *testing.T) {
	s := loginServer(t, "root", "pw123456")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"root","password":"wrong"}`))
	s.handleLogin(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestLoginMissingFields(t *testing.T) {
	s := loginServer(t, "root", "pw123456")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"","password":""}`))
	s.handleLogin(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}
