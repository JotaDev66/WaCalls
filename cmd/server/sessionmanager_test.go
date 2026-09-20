package main

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

func newTestManager(t *testing.T) *SessionManager {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mgr_test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := newSessionStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	return newSessionManager(ctx, container, NewBroker(), store, waLog.Noop, slog.Default(), 0)
}

func (m *SessionManager) addUnconnected(t *testing.T, name string) *Session {
	t.Helper()
	id := newSessionID()
	if err := m.store.insert(m.appCtx, id, name); err != nil {
		t.Fatal(err)
	}
	client := whatsmeow.NewClient(m.container.NewDevice(), waLog.Noop)
	s := newSession(m, id, name, client)
	m.register(s)
	return s
}

func TestSessionManagerRegistry(t *testing.T) {
	m := newTestManager(t)

	if len(m.infos()) != 0 {
		t.Fatal("expected no sessions when empty")
	}

	a := m.addUnconnected(t, "Account A")
	b := m.addUnconnected(t, "Account B")

	infos := m.infos()
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
	if infos[0].Name != "Account A" || infos[1].Name != "Account B" {
		t.Fatalf("registration order not preserved: %+v", infos)
	}
	if infos[0].Paired {
		t.Fatal("unconnected session should not report paired")
	}

	if got, ok := m.Get(a.id); !ok || got != a {
		t.Fatal("Get did not return registered session")
	}

	m.unregister(b.id)
	if _, ok := m.Get(b.id); ok {
		t.Fatal("session b should be gone after unregister")
	}
	if len(m.infos()) != 1 {
		t.Fatal("expected 1 session after unregister")
	}
}
