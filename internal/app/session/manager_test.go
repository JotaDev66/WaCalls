package session

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"wacalls/internal/app/events"
	"wacalls/internal/store/sqlite"

	"go.mau.fi/whatsmeow"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mgr_test.db")
	bundle, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bundle.Close() })
	return NewManager(Deps{
		Ctx: ctx, Container: bundle.Container, Broker: events.NewBroker(bundle.Calls, slog.Default()),
		Store: bundle.Sessions, WALogger: waLog.Noop, Log: slog.Default(), Photos: bundle.Photos,
	})
}

func TestNewSessionID(t *testing.T) {
	id := newSessionID()
	if len(id) != 32 {
		t.Fatalf("session id should be 32 hex chars, got %d", len(id))
	}
}

func (m *Manager) addUnconnected(t *testing.T, name string) *Session {
	t.Helper()
	id := newSessionID()
	if err := m.store.Insert(m.appCtx, id, name); err != nil {
		t.Fatal(err)
	}
	client := whatsmeow.NewClient(m.container.NewDevice(), waLog.Noop)
	return m.NewSession(id, name, client)
}

func TestManagerRegistry(t *testing.T) {
	m := newTestManager(t)

	if len(m.Infos()) != 0 {
		t.Fatal("expected no sessions when empty")
	}

	a := m.addUnconnected(t, "Account A")
	b := m.addUnconnected(t, "Account B")

	infos := m.Infos()
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
	if len(m.Infos()) != 1 {
		t.Fatal("expected 1 session after unregister")
	}
}

func TestManagerRename(t *testing.T) {
	m := newTestManager(t)
	s := m.addUnconnected(t, "Account A")

	if err := m.Rename(context.Background(), s.id, "Sales"); err != nil {
		t.Fatal(err)
	}
	infos := m.Infos()
	if len(infos) != 1 || infos[0].Name != "Sales" {
		t.Fatalf("rename not reflected in Infos: %+v", infos)
	}
	rows, err := m.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Sales" {
		t.Fatalf("rename not persisted: %+v", rows)
	}

	if err := m.Rename(context.Background(), "missing", "X"); err == nil {
		t.Fatal("rename of unknown session must error")
	}
}
