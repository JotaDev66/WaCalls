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
	s := &Server{auth: fa, hasAdmin: true, apiToken: ""}
	s.authorize = s.authorizeRequest
	return s
}

func TestAuthStatusModes(t *testing.T) {
	cases := []struct {
		hasAdmin bool
		token    string
		want     string
	}{
		{false, "", "open"},
		{false, "t", "token"},
		{true, "", "login"},
		{true, "t", "login"},
	}
	for _, c := range cases {
		s := &Server{auth: &fakeAuth{}, hasAdmin: c.hasAdmin, apiToken: c.token}
		rec := httptest.NewRecorder()
		s.handleAuthStatus(rec, httptest.NewRequest("GET", "/api/auth/status", nil))
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Mode != c.want {
			t.Fatalf("hasAdmin=%v token=%q: want mode %q, got %q", c.hasAdmin, c.token, c.want, body.Mode)
		}
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
