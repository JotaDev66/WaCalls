package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			p, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("WACALLS_TRUSTED_PROXIES: %w", err)
			}
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("WACALLS_TRUSTED_PROXIES: %w", err)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

const (
	rateLimitIdleEvict       = 3 * time.Minute
	rateLimitJanitorInterval = time.Minute
)

type ipLimiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

type ipRateLimiter struct {
	mu    sync.Mutex
	perIP map[string]*ipLimiterEntry
	rps   rate.Limit
	burst int
}

const (
	loginRateRPS   = 0.1 // ~6/min sustained
	loginRateBurst = 5
)

func newIPRateLimiter(rps float64) *ipRateLimiter {
	return newIPRateLimiterWithBurst(rps, max(1, int(2*rps)))
}

func newIPRateLimiterWithBurst(rps float64, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		perIP: map[string]*ipLimiterEntry{},
		rps:   rate.Limit(rps),
		burst: burst,
	}
}

func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.perIP[key]
	if !ok {
		e = &ipLimiterEntry{lim: rate.NewLimiter(l.rps, l.burst)}
		l.perIP[key] = e
	}
	e.lastSeen = time.Now()
	return e.lim.Allow()
}

func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !inPrefixes(addr, trusted) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for _, part := range slices.Backward(parts) {
		hop, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			break
		}
		if !inPrefixes(hop, trusted) {
			return hop.String()
		}
	}
	return host
}

func inPrefixes(a netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(a.Unmap()) {
			return true
		}
	}
	return false
}

func (l *ipRateLimiter) purge(idle time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-idle)
	for ip, e := range l.perIP {
		if e.lastSeen.Before(cutoff) {
			delete(l.perIP, ip)
		}
	}
}

func (l *ipRateLimiter) janitor(ctx context.Context) {
	t := time.NewTicker(rateLimitJanitorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.purge(rateLimitIdleEvict)
		}
	}
}

func (s *Server) withRateLimit(next http.Handler) http.Handler {
	if s.rateLimiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimiter.allow(clientIP(r, s.trustedProxies)) {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLoginRateLimit(next http.Handler) http.Handler {
	if s.loginLimiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.loginLimiter.allow(clientIP(r, s.trustedProxies)) {
			w.Header().Set("Retry-After", "5")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many login attempts"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
