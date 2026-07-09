package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenConcurrencyConfig(t *testing.T) {
	bundle, err := Open(context.Background(), filepath.Join(t.TempDir(), "concurrency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	db := bundle.Sessions.(*sessionStore).db

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("expected pool capped to 1 connection, got %d", got)
	}

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL journal mode, got %q", mode)
	}
}
