package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const sessionCookie = "wacalls_session"
const sessionTTL = 7 * 24 * time.Hour

func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("access_token")
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func newSessionToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *Server) tokenMatches(r *http.Request) bool {
	return s.apiToken != "" && subtle.ConstantTimeCompare([]byte(requestToken(r)), []byte(s.apiToken)) == 1
}

func (s *Server) authorizeRequest(r *http.Request) bool {
	if s.hasAdmin {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			if ok, _ := s.auth.SessionValid(r.Context(), hashToken(c.Value), time.Now().Unix()); ok {
				return true
			}
		}
		return s.tokenMatches(r)
	}
	if s.apiToken == "" {
		return true
	}
	return s.tokenMatches(r)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r), MaxAge: int(sessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r), MaxAge: -1,
	})
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
