package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"wacalls/internal/app"
	"wacalls/internal/store"
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

	storeCfg := store.Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		SQLitePath:  *dbPath,
	}
	srv, err := app.NewServer(ctx, storeCfg, *staticDir, *maxCalls, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := srv.Run(ctx, *addr); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}
