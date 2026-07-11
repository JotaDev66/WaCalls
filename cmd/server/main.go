package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wacalls/internal/app"
	"wacalls/internal/store"
	"wacalls/internal/telemetry"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "wacalls.db", "SQLite session database path")
	staticDir := flag.String("static", "client/dist", "static client directory (optional)")
	debug := flag.Bool("debug", false, "verbose logging")
	maxCalls := flag.Int("max-calls-per-session", 8, "max concurrent calls per session (0 = unlimited)")
	unixSocket := flag.String("unix-socket", envOrDefault("WACALLS_SOCKET", "/run/ligacao-ai/wacalls.sock"), "private local PCM Unix socket (empty disables it)")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, obsFactory, tracer, err := telemetry.Init(ctx, telemetry.ConfigFromEnv())
	if err != nil {
		log.Error("telemetry init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(sctx)
	}()

	storeCfg := store.Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		SQLitePath:  *dbPath,
	}
	srv, err := app.NewServer(ctx, storeCfg, *staticDir, *maxCalls, *debug, obsFactory, tracer, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := srv.Run(ctx, *addr, *unixSocket); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
