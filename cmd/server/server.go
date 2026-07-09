package main

import (
	"context"
	"log/slog"

	"wacalls/internal/store/sqlite"

	waLog "go.mau.fi/whatsmeow/util/log"
)

type server struct {
	broker    *Broker
	sessions  *SessionManager
	log       *slog.Logger
	staticDir string
}

func newServer(ctx context.Context, dbPath, staticDir string, maxCalls int, log *slog.Logger) (*server, error) {
	bundle, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "INFO", true)
	}

	broker := NewBroker()
	mgr := newSessionManager(ctx, bundle.Container, broker, bundle.Sessions, waLogger, log, maxCalls)
	broker.SnapshotFn = mgr.snapshotEvents

	return &server{broker: broker, sessions: mgr, log: log, staticDir: staticDir}, nil
}
