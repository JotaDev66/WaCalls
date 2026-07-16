package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wacalls/internal/app"
	"wacalls/internal/app/config"
	"wacalls/internal/app/doctor"
	"wacalls/internal/telemetry"
)

var version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "wacalls.db", "SQLite session database path")
	staticDir := flag.String("static", "client/dist", "static client directory (optional)")
	debug := flag.Bool("debug", false, "verbose logging")
	maxCalls := flag.Int("max-calls-per-session", 8, "max concurrent calls per session (0 = unlimited)")
	showVersion := flag.Bool("version", false, "print version and exit")
	runDoctor := flag.Bool("doctor", false, "run preflight connectivity checks and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("wacalls " + version)
		return
	}

	cfg := config.LoadConfig(*addr, *dbPath, *staticDir, *debug, *maxCalls)
	cfg.Version = version

	if *runDoctor {
		if !doctor.Doctor(context.Background(), cfg, os.Stdout) {
			os.Exit(1)
		}
		return
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	log.Info("wacalls starting", "version", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tcfg := telemetry.ConfigFromEnv()
	tcfg.ServiceVersion = version
	shutdown, obsFactory, tracer, err := telemetry.Init(ctx, tcfg)
	if err != nil {
		log.Error("telemetry init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(sctx)
	}()

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
