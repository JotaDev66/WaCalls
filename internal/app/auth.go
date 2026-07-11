package app

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("access_token")
}

func bearerAuthorizer(token string) func(*http.Request) bool {
	if token == "" {
		return func(*http.Request) bool { return true }
	}
	want := []byte(token)
	return func(r *http.Request) bool {
		return subtle.ConstantTimeCompare([]byte(requestToken(r)), want) == 1
	}
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorize(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}
