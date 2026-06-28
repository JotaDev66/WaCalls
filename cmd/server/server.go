package main

import (
	"context"
	"database/sql"
	"log/slog"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

type server struct {
	broker        *Broker
	sessions      *SessionManager
	log           *slog.Logger
	staticDir     string
	recordingsDir string
}

func openDB(dbPath string) (*sql.DB, error) {
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func newServer(ctx context.Context, dbPath, staticDir string, maxCalls int, recordingsDir string, log *slog.Logger) (*server, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		return nil, err
	}
	store, err := newSessionStore(ctx, db)
	if err != nil {
		return nil, err
	}

	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "INFO", true)
	}

	broker := NewBroker()
	mgr := newSessionManager(ctx, container, broker, store, waLogger, log, maxCalls)
	broker.SnapshotFn = mgr.snapshotEvents

	return &server{broker: broker, sessions: mgr, log: log, staticDir: staticDir, recordingsDir: recordingsDir}, nil
}
