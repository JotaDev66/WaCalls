package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	loadDotEnv(".env")

	// Defaults come from .env / the environment; explicit flags override them.
	addr := flag.String("addr", listenAddr(), "HTTP listen address (or set PORT)")
	dbPath := flag.String("db", getenv("DB_PATH", "wacalls.db"), "SQLite session database path")
	staticDir := flag.String("static", getenv("STATIC_DIR", "client/dist"), "static client directory (optional)")
	debug := flag.Bool("debug", getenvBool("DEBUG", false), "verbose logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := newServer(ctx, *dbPath, *staticDir, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer srv.sessions.disconnectAll()

	if err := srv.sessions.Restore(ctx); err != nil {
		log.Error("session restore failed", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{Addr: *addr, Handler: srv.routes()}
	go func() {
		log.Info("HTTP server listening", "addr", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
