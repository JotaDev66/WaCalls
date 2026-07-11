package app

import (
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
