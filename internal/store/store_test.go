package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"wacalls/internal/store"
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
			t.Cleanup(func() { bundle.Close() })
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
