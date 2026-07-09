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

func TestSessionStoreRoundtrip(t *testing.T) {
	ctx := context.Background()
	bundle, err := Open(ctx, filepath.Join(t.TempDir(), "sessions_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	st := bundle.Sessions

	id := "session-a"
	if err := st.Insert(ctx, id, "Account A"); err != nil {
		t.Fatal(err)
	}

	rows, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id || rows[0].Name != "Account A" || rows[0].JID != "" {
		t.Fatalf("unexpected rows after insert: %+v", rows)
	}

	if err := st.SetJID(ctx, id, "5511999999999:1@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.List(ctx)
	if rows[0].JID != "5511999999999:1@s.whatsapp.net" {
		t.Fatalf("jid not persisted: %+v", rows[0])
	}

	if err := st.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.List(ctx)
	if len(rows) != 0 {
		t.Fatalf("expected empty after delete, got %+v", rows)
	}
}
