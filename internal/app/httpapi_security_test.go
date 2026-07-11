package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAuthProtectsAPIInConstantScope(t *testing.T) {
	handler := withAdminAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "secret-token")

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "wrong", header: "Bearer other", want: http.StatusUnauthorized},
		{name: "valid", header: "Bearer secret-token", want: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/sessions", nil)
			req.Header.Set("Authorization", tc.header)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d want=%d", res.Code, tc.want)
			}
		})
	}
}

func TestCORSAllowsOnlyExactSameOrigin(t *testing.T) {
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8080/api/sessions", nil)
	allowed.Header.Set("Origin", "http://127.0.0.1:8080")
	allowedResult := httptest.NewRecorder()
	handler.ServeHTTP(allowedResult, allowed)
	if allowedResult.Code != http.StatusNoContent || allowedResult.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:8080" {
		t.Fatalf("same-origin preflight failed: status=%d origin=%q", allowedResult.Code, allowedResult.Header().Get("Access-Control-Allow-Origin"))
	}

	denied := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/sessions", nil)
	denied.Header.Set("Origin", "https://evil.example")
	deniedResult := httptest.NewRecorder()
	handler.ServeHTTP(deniedResult, denied)
	if deniedResult.Code != http.StatusForbidden || deniedResult.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("cross-origin request was not denied: status=%d origin=%q", deniedResult.Code, deniedResult.Header().Get("Access-Control-Allow-Origin"))
	}
}
