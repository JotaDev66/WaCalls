package app

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

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

func newIPRateLimiter(rps float64) *ipRateLimiter {
	burst := int(2 * rps)
	if burst < 1 {
		burst = 1
	}
	return &ipRateLimiter{
		perIP: map[string]*ipLimiterEntry{},
		rps:   rate.Limit(rps),
		burst: burst,
	}
}

func (l *ipRateLimiter) allow(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.perIP[host]
	if !ok {
		e = &ipLimiterEntry{lim: rate.NewLimiter(l.rps, l.burst)}
		l.perIP[host] = e
	}
	e.lastSeen = time.Now()
	return e.lim.Allow()
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
		if !s.rateLimiter.allow(r.RemoteAddr) {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
