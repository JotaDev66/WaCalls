package app

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/pprof"
	"os"

	"wacalls/internal/app/webui"
)

var apiRoutes = []struct {
	method, path string
	handler      func(*Server, http.ResponseWriter, *http.Request)
}{
	{"GET", "/sessions", (*Server).handleSessionList},
	{"POST", "/sessions", (*Server).handleSessionCreate},
	{"DELETE", "/sessions/{sid}", (*Server).handleSessionDelete},
	{"POST", "/sessions/{sid}/logout", (*Server).handleSessionLogout},
	{"POST", "/sessions/{sid}/pair", (*Server).handleSessionPair},
	{"POST", "/sessions/{sid}/calls", (*Server).handleStartCall},
	{"GET", "/sessions/{sid}/calls", (*Server).handleCallList},
	{"GET", "/sessions/{sid}/calls/{id}", (*Server).handleCallGet},
	{"POST", "/sessions/{sid}/calls/{id}/webrtc", (*Server).handleWebRTC},
	{"POST", "/sessions/{sid}/calls/{id}/accept", (*Server).handleAccept},
	{"POST", "/sessions/{sid}/calls/{id}/reject", (*Server).handleReject},
	{"DELETE", "/sessions/{sid}/calls/{id}", (*Server).handleEndCall},
	{"GET", "/sessions/{sid}/history", (*Server).handleHistory},
	{"GET", "/sessions/{sid}/history/export", (*Server).handleHistoryExport},
	{"GET", "/sessions/{sid}/contacts", (*Server).handleContactList},
	{"POST", "/sessions/{sid}/contacts", (*Server).handleContactSave},
	{"GET", "/version", (*Server).handleVersion},
	{"POST", "/logout", (*Server).handleLogout},
	{"POST", "/auth/password", (*Server).handlePassword},
	{"GET", "/events", (*Server).handleEvents},
}

func (s *Server) routes() http.Handler {
	api := http.NewServeMux()

	for _, rt := range apiRoutes {
		api.HandleFunc(rt.method+" /api"+rt.path, func(w http.ResponseWriter, r *http.Request) {
			rt.handler(s, w, r)
		})
	}

	if s.debug {
		api.HandleFunc("GET /debug/pprof/", pprof.Index)
		api.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		api.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		api.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		api.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", handleHealthz)
	root.HandleFunc("GET /api/openapi.yaml", handleOpenAPI)
	root.Handle("GET /api/auth/status", s.withRateLimit(maxBytes(http.HandlerFunc(s.handleAuthStatus))))
	root.Handle("POST /api/login", s.withLoginRateLimit(s.withRateLimit(maxBytes(http.HandlerFunc(s.handleLogin)))))
	root.Handle("/api/", s.withRateLimit(s.withAuth(maxBytes(api))))
	if s.debug {
		root.Handle("/debug/", loopbackOnly(s.withAuth(api)))
	}
	root.Handle("/", s.uiHandler())
	return s.withCORS(root)
}

const maxBodyBytes = 1 << 20

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func maxBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) uiHandler() http.Handler {
	if s.staticDir != "" {
		if _, err := os.Stat(s.staticDir); err == nil {
			return http.FileServer(http.Dir(s.staticDir))
		}
	}
	if sub, err := webui.FS(); err == nil {
		return http.FileServerFS(sub)
	}
	return http.NotFoundHandler()
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := s.allowedOrigins[origin]; ok && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Client-Id, Authorization")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func clientID(r *http.Request) string {
	if id := r.Header.Get("X-Client-Id"); id != "" {
		return id
	}
	return r.URL.Query().Get("clientId")
}

func (s *Server) sessionByID(w http.ResponseWriter, sid string) *Session {
	sess, ok := s.sessions.Get(sid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return nil
	}
	return sess
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.broker.ServeSSE(w, r, clientID(r))
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}
