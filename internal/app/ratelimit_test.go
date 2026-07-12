package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIPRateLimiterBurstAndDeny(t *testing.T) {
	l := newIPRateLimiter(1)
	for i := range 2 {
		if !l.allow("10.0.0.1:1234") {
			t.Fatalf("burst request %d should be allowed", i)
		}
	}
	if l.allow("10.0.0.1:1234") {
		t.Fatal("request beyond burst should be denied")
	}
	if !l.allow("10.0.0.2:9999") {
		t.Fatal("distinct IP must have its own bucket")
	}
}

func TestIPRateLimiterRefill(t *testing.T) {
	l := newIPRateLimiter(100)
	for range 200 {
		l.allow("10.0.0.3:1")
	}
	if l.allow("10.0.0.3:1") {
		t.Fatal("bucket should be empty")
	}
	time.Sleep(50 * time.Millisecond)
	if !l.allow("10.0.0.3:1") {
		t.Fatal("bucket should refill over time")
	}
}

func TestIPRateLimiterPurge(t *testing.T) {
	l := newIPRateLimiter(1)
	l.allow("10.0.0.4:1")
	l.allow("10.0.0.5:1")
	l.mu.Lock()
	l.perIP["10.0.0.4"].lastSeen = time.Now().Add(-10 * time.Minute)
	l.mu.Unlock()
	l.purge(3 * time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.perIP["10.0.0.4"]; ok {
		t.Fatal("idle entry should be purged")
	}
	if _, ok := l.perIP["10.0.0.5"]; !ok {
		t.Fatal("active entry should survive purge")
	}
}

func TestWithRateLimitNilPassthrough(t *testing.T) {
	s := &Server{}
	h := s.withRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("nil limiter must pass through, got %d", rec.Code)
	}
}

func TestWithRateLimit429(t *testing.T) {
	s := &Server{rateLimiter: newIPRateLimiter(1)}
	h := s.withRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.RemoteAddr = "192.0.2.1:5555"
	for i := range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d should pass, got %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("want Retry-After 1, got %q", rec.Header().Get("Retry-After"))
	}
	if !strings.Contains(rec.Body.String(), "rate limit exceeded") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestRateLimitAppliesBeforeAuth(t *testing.T) {
	s := &Server{
		authorize:   bearerAuthorizer("secret"),
		rateLimiter: newIPRateLimiter(1),
	}
	h := s.routes()
	codes := []int{}
	for range 3 {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		req.RemoteAddr = "192.0.2.9:1111"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	want := []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %v, want %v (unauthorized must consume budget)", codes, want)
		}
	}
}

func TestPreflightBypassesRateLimit(t *testing.T) {
	s := &Server{
		authorize:      bearerAuthorizer(""),
		rateLimiter:    newIPRateLimiter(1),
		allowedOrigins: map[string]struct{}{"https://app.example.com": {}},
	}
	h := s.routes()
	for i := range 5 {
		req := httptest.NewRequest(http.MethodOptions, "/api/sessions", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.RemoteAddr = "192.0.2.7:2222"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("preflight %d = %d, want 204 (must not consume budget)", i, rec.Code)
		}
	}
}
