package app

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestLoginLimiterBurstThenBlocks(t *testing.T) {
	l := newIPRateLimiterWithBurst(0.0001, 3)
	for i := 0; i < 3; i++ {
		if !l.allow("ip") {
			t.Fatalf("attempt %d should pass within burst", i)
		}
	}
	if l.allow("ip") {
		t.Fatal("4th attempt should be blocked")
	}
}

func TestParseTrustedProxies(t *testing.T) {
	got, err := parseTrustedProxies(" 127.0.0.1, 10.0.0.0/8 , ::1 ")
	if err != nil || len(got) != 3 {
		t.Fatalf("got %v err %v", got, err)
	}
	if got[0].String() != "127.0.0.1/32" || got[1].String() != "10.0.0.0/8" || got[2].String() != "::1/128" {
		t.Fatalf("prefixes: %v", got)
	}
	if out, err := parseTrustedProxies(""); err != nil || out != nil {
		t.Fatalf("empty must be nil, got %v err %v", out, err)
	}
	if _, err := parseTrustedProxies("banana"); err == nil {
		t.Fatal("invalid entry must error")
	}
	if _, err := parseTrustedProxies("10.0.0.0/99"); err == nil {
		t.Fatal("invalid cidr must error")
	}
}

func TestIPRateLimiterBurstAndDeny(t *testing.T) {
	l := newIPRateLimiter(1)
	for i := range 2 {
		if !l.allow("10.0.0.1") {
			t.Fatalf("burst request %d should be allowed", i)
		}
	}
	if l.allow("10.0.0.1") {
		t.Fatal("request beyond burst should be denied")
	}
	if !l.allow("10.0.0.2") {
		t.Fatal("distinct IP must have its own bucket")
	}
}

func TestIPRateLimiterRefill(t *testing.T) {
	l := newIPRateLimiter(100)
	for range 200 {
		l.allow("10.0.0.3")
	}
	if l.allow("10.0.0.3") {
		t.Fatal("bucket should be empty")
	}
	time.Sleep(50 * time.Millisecond)
	if !l.allow("10.0.0.3") {
		t.Fatal("bucket should refill over time")
	}
}

func TestIPRateLimiterPurge(t *testing.T) {
	l := newIPRateLimiter(1)
	l.allow("10.0.0.4")
	l.allow("10.0.0.5")
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

func trustedReq(remote, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIPKeying(t *testing.T) {
	trusted, _ := parseTrustedProxies("127.0.0.1, 10.0.0.0/8")
	cases := []struct {
		name, remote, xff, want string
		trusted                 []netip.Prefix
	}{
		{"no trusted list", "203.0.113.9:1", "6.6.6.6", "203.0.113.9", nil},
		{"trusted no xff", "127.0.0.1:1", "", "127.0.0.1", trusted},
		{"trusted simple xff", "127.0.0.1:1", "203.0.113.7", "203.0.113.7", trusted},
		{"spoofed left entries ignored", "127.0.0.1:1", "6.6.6.6, 1.2.3.4", "1.2.3.4", trusted},
		{"chained trusted proxies", "127.0.0.1:1", "1.2.3.4, 10.0.0.5", "1.2.3.4", trusted},
		{"all entries trusted falls back", "127.0.0.1:1", "10.0.0.5, 10.0.0.6", "127.0.0.1", trusted},
		{"untrusted source xff ignored", "203.0.113.9:1", "1.2.3.4", "203.0.113.9", trusted},
		{"malformed xff falls back", "127.0.0.1:1", "not-an-ip", "127.0.0.1", trusted},
	}
	for _, tc := range cases {
		if got := clientIP(trustedReq(tc.remote, tc.xff), tc.trusted); got != tc.want {
			t.Fatalf("%s: want %s, got %s", tc.name, tc.want, got)
		}
	}
}

func TestWithRateLimitTrustedProxyBuckets(t *testing.T) {
	trusted, _ := parseTrustedProxies("127.0.0.1")
	s := &Server{rateLimiter: newIPRateLimiter(1), trustedProxies: trusted}
	h := s.withRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	hit := func(xff string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, trustedReq("127.0.0.1:9", xff))
		return rec.Code
	}
	hit("203.0.113.7")
	hit("203.0.113.7")
	if code := hit("203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("third hit same client: want 429, got %d", code)
	}
	if code := hit("203.0.113.8"); code != http.StatusOK {
		t.Fatalf("distinct forwarded client must have its own bucket, got %d", code)
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

func TestHealthzBypassesRateLimit(t *testing.T) {
	s := &Server{
		authorize:   bearerAuthorizer(""),
		rateLimiter: newIPRateLimiter(1),
	}
	h := s.routes()
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "192.0.2.8:3333"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("healthz %d = %d, want 200 (must not consume budget)", i, rec.Code)
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
