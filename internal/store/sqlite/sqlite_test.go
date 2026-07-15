package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"wacalls/internal/voip/core"
)

func TestOpenConcurrencyConfig(t *testing.T) {
	bundle, err := Open(context.Background(), filepath.Join(t.TempDir(), "concurrency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()
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

func TestAuthStore(t *testing.T) {
	ctx := context.Background()
	b, err := Open(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	a := b.Auth

	if _, ok, _ := a.GetAdmin(ctx); ok {
		t.Fatal("no admin expected initially")
	}
	if err := a.CreateAdmin(ctx, "root", "hash1"); err != nil {
		t.Fatal(err)
	}
	cred, ok, err := a.GetAdmin(ctx)
	if err != nil || !ok || cred.Username != "root" || cred.PasswordHash != "hash1" {
		t.Fatalf("get admin: %+v ok=%v err=%v", cred, ok, err)
	}
	if err := a.SetAdminPassword(ctx, "hash2"); err != nil {
		t.Fatal(err)
	}
	if cred, _, _ := a.GetAdmin(ctx); cred.PasswordHash != "hash2" {
		t.Fatalf("password not updated: %q", cred.PasswordHash)
	}

	if err := a.CreateSession(ctx, "tokA", 1000); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateSession(ctx, "tokB", 1000); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.SessionValid(ctx, "tokA", 999); !ok {
		t.Fatal("tokA should be valid at now<exp")
	}
	if ok, _ := a.SessionValid(ctx, "tokA", 1000); ok {
		t.Fatal("tokA should be invalid at now==exp")
	}
	if err := a.DeleteSessionsExcept(ctx, "tokA"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.SessionValid(ctx, "tokB", 999); ok {
		t.Fatal("tokB should be gone")
	}
	if ok, _ := a.SessionValid(ctx, "tokA", 999); !ok {
		t.Fatal("tokA should remain")
	}
	_ = a.CreateSession(ctx, "old", 10)
	if err := a.PurgeExpiredSessions(ctx, 999); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.SessionValid(ctx, "old", 5); ok {
		t.Fatal("expired purged even against past now")
	}
}

func TestContactPhotoStore(t *testing.T) {
	b, err := Open(context.Background(), filepath.Join(t.TempDir(), "photos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	ctx := context.Background()
	jid := "5511@s.whatsapp.net"
	p := core.ContactPhoto{SessionID: "s1", Jid: jid, URL: "u1", PictureID: "id1", FetchedAt: 10}
	if err := b.Photos.Upsert(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := b.Photos.Get(ctx, "s1", jid)
	if err != nil || !ok || got.URL != "u1" {
		t.Fatalf("get: %+v ok=%v err=%v", got, ok, err)
	}
	p.URL, p.PictureID = "u2", "id2"
	if err := b.Photos.Upsert(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, _, _ = b.Photos.Get(ctx, "s1", jid)
	if got.URL != "u2" || got.PictureID != "id2" {
		t.Fatalf("conflict update failed: %+v", got)
	}
	if _, ok, _ := b.Photos.Get(ctx, "s1", "absent"); ok {
		t.Fatal("absent jid should not be found")
	}
	m, err := b.Photos.GetMany(ctx, "s1", []string{jid, "absent"})
	if err != nil || len(m) != 1 || m[jid].URL != "u2" {
		t.Fatalf("getmany: %+v err=%v", m, err)
	}
	if m2, err := b.Photos.GetMany(ctx, "s1", nil); err != nil || len(m2) != 0 {
		t.Fatalf("getmany empty: %+v err=%v", m2, err)
	}
}
