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
	"wacalls/internal/telemetry"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "wacalls.db", "SQLite session database path")
	staticDir := flag.String("static", "client/dist", "static client directory (optional)")
	debug := flag.Bool("debug", false, "verbose logging")
	maxCalls := flag.Int("max-calls-per-session", 8, "max concurrent calls per session (0 = unlimited)")
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

	cfg := app.LoadConfig(*addr, *dbPath, *staticDir, *debug, *maxCalls)
	srv, err := app.NewServer(ctx, cfg, obsFactory, tracer, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := srv.Run(ctx, cfg.Addr); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}
