package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"wacalls/internal/store"
	"wacalls/internal/voip/core"
)

func TestSessionStoreContract(t *testing.T) {
	cases := []struct {
		name string
		cfg  func(t *testing.T) store.Config
	}{
		{"sqlite", func(t *testing.T) store.Config {
			return store.Config{SQLitePath: filepath.Join(t.TempDir(), "contract.db")}
		}},
		{"postgres", func(t *testing.T) store.Config {
			url := os.Getenv("WACALLS_TEST_DATABASE_URL")
			if url == "" {
				t.Skip("set WACALLS_TEST_DATABASE_URL to run the postgres contract test")
			}
			return store.Config{DatabaseURL: url}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			bundle, err := store.Open(ctx, tc.cfg(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = bundle.Close() })
			st := bundle.Sessions

			existing, err := st.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range existing {
				if err := st.Delete(ctx, r.ID); err != nil {
					t.Fatal(err)
				}
			}

			if err := st.Insert(ctx, "a", "Account A"); err != nil {
				t.Fatal(err)
			}
			if err := st.Insert(ctx, "b", "Account B"); err != nil {
				t.Fatal(err)
			}

			rows, err := st.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || rows[0].ID != "a" || rows[1].ID != "b" {
				t.Fatalf("expected insertion order [a b], got %+v", rows)
			}
			if rows[0].JID != "" {
				t.Fatalf("expected empty jid after insert, got %q", rows[0].JID)
			}

			if err := st.SetJID(ctx, "a", "5511999999999:1@s.whatsapp.net"); err != nil {
				t.Fatal(err)
			}
			rows, _ = st.List(ctx)
			if rows[0].JID != "5511999999999:1@s.whatsapp.net" {
				t.Fatalf("jid not persisted: %+v", rows[0])
			}

			if err := st.Delete(ctx, "a"); err != nil {
				t.Fatal(err)
			}
			rows, _ = st.List(ctx)
			if len(rows) != 1 || rows[0].ID != "b" {
				t.Fatalf("expected [b] after delete, got %+v", rows)
			}

			if err := st.Delete(ctx, "b"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCallRecordStoreContract(t *testing.T) {
	cases := []struct {
		name string
		cfg  func(t *testing.T) store.Config
	}{
		{"sqlite", func(t *testing.T) store.Config {
			return store.Config{SQLitePath: filepath.Join(t.TempDir(), "records.db")}
		}},
		{"postgres", func(t *testing.T) store.Config {
			url := os.Getenv("WACALLS_TEST_DATABASE_URL")
			if url == "" {
				t.Skip("set WACALLS_TEST_DATABASE_URL to run the postgres contract test")
			}
			return store.Config{DatabaseURL: url}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			bundle, err := store.Open(ctx, tc.cfg(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = bundle.Close() })
			st := bundle.Calls

			if err := st.Prune(ctx, 0); err != nil {
				t.Fatal(err)
			}

			owner := "op-A"
			recs := []core.CallRecord{
				{CallID: "c1", SessionID: "s1", Owner: &owner, Direction: "outbound", Peer: "111@s.whatsapp.net", StartedAt: 100, EndedAt: 200, EndReason: "user_ended"},
				{CallID: "c2", SessionID: "s1", Direction: "inbound", Peer: "222@s.whatsapp.net", StartedAt: 150, EndedAt: 300, EndReason: "timeout"},
				{CallID: "c3", SessionID: "s2", Direction: "inbound", Peer: "333@s.whatsapp.net", StartedAt: 180, EndedAt: 250, EndReason: "declined"},
			}
			for _, r := range recs {
				if err := st.Insert(ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.Insert(ctx, recs[0]); err != nil {
				t.Fatalf("duplicate insert must not error: %v", err)
			}

			all, err := st.List(ctx, "", 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != 3 || all[0].CallID != "c2" || all[1].CallID != "c3" || all[2].CallID != "c1" {
				t.Fatalf("expected ended_at DESC [c2 c3 c1], got %+v", all)
			}
			if all[2].Owner == nil || *all[2].Owner != "op-A" {
				t.Fatalf("owner not round-tripped: %+v", all[2])
			}
			if all[0].Owner != nil {
				t.Fatalf("nil owner must stay nil: %+v", all[0])
			}

			s1, err := st.List(ctx, "s1", 10)
			if err != nil || len(s1) != 2 || s1[0].CallID != "c2" || s1[1].CallID != "c1" {
				t.Fatalf("expected s1 [c2 c1], got %+v err %v", s1, err)
			}
			limited, err := st.List(ctx, "s1", 1)
			if err != nil || len(limited) != 1 || limited[0].CallID != "c2" {
				t.Fatalf("expected limit 1 [c2], got %+v err %v", limited, err)
			}

			if err := st.Prune(ctx, 2); err != nil {
				t.Fatal(err)
			}
			all, err = st.List(ctx, "", 10)
			if err != nil || len(all) != 2 || all[0].CallID != "c2" || all[1].CallID != "c3" {
				t.Fatalf("prune must keep 2 most recent [c2 c3], got %+v err %v", all, err)
			}
		})
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "migrate.db")

	b1, err := store.Open(ctx, store.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := b1.Sessions.Insert(ctx, "keep", "Keep"); err != nil {
		t.Fatal(err)
	}
	if err := b1.Calls.Insert(ctx, core.CallRecord{CallID: "c1", SessionID: "keep", Direction: "inbound", Peer: "p", StartedAt: 1, EndedAt: 2}); err != nil {
		t.Fatal(err)
	}
	if err := b1.Close(); err != nil {
		t.Fatal(err)
	}

	b2, err := store.Open(ctx, store.Config{SQLitePath: path})
	if err != nil {
		t.Fatalf("second open must not fail: %v", err)
	}
	defer func() { _ = b2.Close() }()
	rows, err := b2.Sessions.List(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != "keep" {
		t.Fatalf("sessions must survive re-open, got %+v err %v", rows, err)
	}
	recs, err := b2.Calls.List(ctx, "", 10)
	if err != nil || len(recs) != 1 || recs[0].CallID != "c1" {
		t.Fatalf("call records must survive re-open, got %+v err %v", recs, err)
	}
}
