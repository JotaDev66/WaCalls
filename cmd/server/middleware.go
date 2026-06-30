package main

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// securityHeaders adds common security-related headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// HSTS: only enable if server is behind TLS
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		// Content Security Policy: restrict to same origin by default
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data:; style-src 'self' 'unsafe-inline';")
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware enforces a whitelist of allowed origins (configurable via ALLOWED_ORIGINS env).
// If ALLOWED_ORIGINS is empty, CORS is not set (same-origin only).
func corsMiddleware(next http.Handler) http.Handler {
	allowedEnv := os.Getenv("ALLOWED_ORIGINS")
	if allowedEnv == "" {
		return next
	}
	allowed := map[string]struct{}{}
	for _, o := range strings.Split(allowedEnv, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Client-Id, Authorization")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			} else {
				// origin not allowed
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware enforces a simple bearer token auth using ADMIN_TOKEN env var.
// If ADMIN_TOKEN is not set, the middleware is a no-op (for developer convenience).
func authMiddleware(next http.Handler) http.Handler {
	token := os.Getenv("ADMIN_TOKEN")
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow health checks or non-api static assets if needed: require auth for /api and /api/events and static files
		// Check Authorization header Bearer token
		auth := r.Header.Get("Authorization")
		if auth != "" {
			if strings.HasPrefix(auth, "Bearer ") {
				if strings.TrimPrefix(auth, "Bearer ") == token {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		// Fallback to cookie
		if c, err := r.Cookie("wacalls_session"); err == nil {
			if c.Value == token {
				next.ServeHTTP(w, r)
				return
			}
		}
		// Not authorized
		w.Header().Set("WWW-Authenticate", "Bearer realm=\"wacalls\"")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	})
}

// Rate limiting per IP

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors   = make(map[string]*visitor)
	visitorsMu sync.Mutex
)

// getVisitor returns a rate limiter for the given IP.
func getVisitor(ip string) *rate.Limiter {
	visitorsMu.Lock()
	defer visitorsMu.Unlock()
	v, exists := visitors[ip]
	if !exists {
		lim := rate.NewLimiter(10, 20) // 10 req/s, burst 20
		visitors[ip] = &visitor{limiter: lim, lastSeen: time.Now()}
		return lim
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// cleanupVisitors periodically cleans up old entries.
func init() {
	go func() {
		for {
			time.Sleep(time.Minute)
			visitorsMu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(visitors, ip)
				}
			}
			visitorsMu.Unlock()
		}
	}()
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		lim := getVisitor(ip)
		if !lim.Allow() {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("too many requests"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
