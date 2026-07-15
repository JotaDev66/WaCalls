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
