package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"wacalls/internal/store"
	"wacalls/internal/telemetry"
	"wacalls/internal/voip/core"

	"github.com/pion/webrtc/v4"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 15 * time.Second
	idleTimeout       = 120 * time.Second
)

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}
}

type Server struct {
	broker         *Broker
	sessions       *SessionManager
	log            *slog.Logger
	staticDir      string
	debug          bool
	authorize      func(*http.Request) bool
	allowedOrigins map[string]struct{}
	webrtcAPI      *webrtc.API
	rateLimiter    *ipRateLimiter
}

func parseOrigins(raw string) map[string]struct{} {
	set := map[string]struct{}{}
	for o := range strings.SplitSeq(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			set[o] = struct{}{}
		}
	}
	return set
}

func NewServer(ctx context.Context, cfg Config, obsFactory func(string) core.CallObserver, tracer telemetry.CallTracer, log *slog.Logger) (*Server, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	bundle, err := store.Open(ctx, store.Config{DatabaseURL: cfg.DatabaseURL, SQLitePath: cfg.DBPath})
	if err != nil {
		return nil, err
	}

	api, err := buildBrowserAPI(cfg.WebRTCUDPPort, cfg.PublicIPs)
	if err != nil {
		return nil, err
	}

	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "INFO", true)
	}

	broker := NewBroker(bundle.Calls, log)
	mgr := newSessionManager(ctx, bundle.Container, broker, bundle.Sessions, waLogger, log, cfg.MaxCalls, obsFactory, tracer)
	broker.SnapshotFn = mgr.snapshotEvents

	var limiter *ipRateLimiter
	if cfg.RateLimit > 0 {
		limiter = newIPRateLimiter(cfg.RateLimit)
		go limiter.janitor(ctx)
	}

	return &Server{
		broker:         broker,
		sessions:       mgr,
		log:            log,
		staticDir:      cfg.StaticDir,
		debug:          cfg.Debug,
		authorize:      bearerAuthorizer(cfg.APIToken),
		allowedOrigins: parseOrigins(cfg.CORSOrigins),
		webrtcAPI:      api,
		rateLimiter:    limiter,
	}, nil
}

func (s *Server) Run(ctx context.Context, addr string) error {
	defer s.sessions.disconnectAll()
	if err := s.sessions.Restore(ctx); err != nil {
		return err
	}
	httpSrv := newHTTPServer(addr, s.routes())
	go func() {
		s.log.Info("HTTP server listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http server error", "err", err)
		}
	}()
	<-ctx.Done()
	s.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}
