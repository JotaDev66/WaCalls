package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"wacalls/internal/store"
	"wacalls/internal/telemetry"
	"wacalls/internal/voip/core"

	waLog "go.mau.fi/whatsmeow/util/log"
)

type Server struct {
	broker     *Broker
	sessions   *SessionManager
	log        *slog.Logger
	staticDir  string
	debug      bool
	adminToken string
}

func NewServer(ctx context.Context, storeCfg store.Config, staticDir string, maxCalls int, debug bool, obsFactory func(string) core.CallObserver, tracer telemetry.CallTracer, log *slog.Logger) (*Server, error) {
	bundle, err := store.Open(ctx, storeCfg)
	if err != nil {
		return nil, err
	}

	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "INFO", true)
	}

	broker := NewBroker()
	mgr := newSessionManager(ctx, bundle.Container, broker, bundle.Sessions, waLogger, log, maxCalls, obsFactory, tracer)
	broker.SnapshotFn = mgr.snapshotEvents

	return &Server{
		broker: broker, sessions: mgr, log: log, staticDir: staticDir,
		debug: debug, adminToken: os.Getenv("WACALLS_ADMIN_TOKEN"),
	}, nil
}

func (s *Server) Run(ctx context.Context, addr, unixSocketPath string) error {
	defer s.sessions.disconnectAll()
	if err := s.sessions.Restore(ctx); err != nil {
		return err
	}
	var unixServer *unixPCMServer
	if unixSocketPath != "" {
		unixServer = newUnixPCMServer(unixSocketPath, sessionLocalBackend{sessions: s.sessions}, s.log)
		if err := unixServer.Start(ctx); err != nil {
			return err
		}
		defer func() { _ = unixServer.Close() }()
	}
	httpSrv := &http.Server{Addr: addr, Handler: s.routes()}
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
