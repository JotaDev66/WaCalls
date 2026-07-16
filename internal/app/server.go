package app

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"wacalls/internal/app/config"
	"wacalls/internal/app/events"
	"wacalls/internal/store"
	"wacalls/internal/telemetry"
	"wacalls/internal/voip/core"

	waLog "go.mau.fi/whatsmeow/util/log"
	"golang.org/x/crypto/bcrypt"
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
	broker         *events.Broker
	sessions       *SessionManager
	log            *slog.Logger
	staticDir      string
	version        string
	debug          bool
	authorize      func(*http.Request) bool
	allowedOrigins map[string]struct{}
	rateLimiter    *ipRateLimiter
	trustedProxies []netip.Prefix
	photos         core.ContactPhotoStore
	auth           core.AuthStore
	apiToken       string
	loginLimiter   *ipRateLimiter
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

func NewServer(ctx context.Context, cfg config.Config, obsFactory func(string) core.CallObserver, tracer telemetry.CallTracer, log *slog.Logger) (*Server, error) {
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	bundle, err := store.Open(ctx, store.Config{DatabaseURL: cfg.DatabaseURL, SQLitePath: cfg.DBPath})
	if err != nil {
		return nil, err
	}

	_, hasAdmin, err := bundle.Auth.GetAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if !hasAdmin && cfg.AdminUser != "" && cfg.AdminPassword != "" {
		hash, herr := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if herr != nil {
			return nil, herr
		}
		if err := bundle.Auth.CreateAdmin(ctx, cfg.AdminUser, string(hash)); err != nil {
			return nil, err
		}
		hasAdmin = true
	}
	if !hasAdmin {
		return nil, errors.New("no admin configured: set WACALLS_ADMIN_USER and WACALLS_ADMIN_PASSWORD to create the initial admin")
	}

	api, err := buildBrowserAPI(cfg.WebRTCUDPPort, cfg.PublicIPs)
	if err != nil {
		return nil, err
	}

	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "INFO", true)
	}

	broker := events.NewBroker(bundle.Calls, log)
	mgr := newSessionManager(ctx, bundle.Container, api, broker, bundle.Sessions, waLogger, log, cfg.MaxCalls, obsFactory, tracer, bundle.Photos)
	broker.SnapshotFn = mgr.snapshotEvents

	if broker.EnableWebhooks(ctx, cfg.WebhookURL, cfg.WebhookSecret) {
		log.Info("webhook delivery enabled", "url", cfg.WebhookURL)
	}

	var limiter *ipRateLimiter
	if cfg.RateLimit > 0 {
		limiter = newIPRateLimiter(cfg.RateLimit)
		go limiter.janitor(ctx)
	}

	trustedProxies, err := parseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		broker:         broker,
		sessions:       mgr,
		log:            log,
		staticDir:      cfg.StaticDir,
		version:        cmp.Or(cfg.Version, "dev"),
		debug:          cfg.Debug,
		allowedOrigins: parseOrigins(cfg.CORSOrigins),
		rateLimiter:    limiter,
		trustedProxies: trustedProxies,
		photos:         bundle.Photos,
		auth:           bundle.Auth,
		apiToken:       cfg.APIToken,
		loginLimiter:   newIPRateLimiterWithBurst(loginRateRPS, loginRateBurst),
	}
	go srv.loginLimiter.janitor(ctx)
	srv.authorize = srv.authorizeRequest
	return srv, nil
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
