package store

import (
	"context"
	"io"
	"strings"

	"wacalls/internal/store/postgres"
	"wacalls/internal/store/sqlite"
	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow/store/sqlstore"
)

type Bundle struct {
	Container *sqlstore.Container
	Sessions  core.SessionStore
	closer    io.Closer
}

func (b *Bundle) Close() error { return b.closer.Close() }

type Config struct {
	DatabaseURL string
	SQLitePath  string
}

func Open(ctx context.Context, cfg Config) (*Bundle, error) {
	if url := strings.TrimSpace(cfg.DatabaseURL); url != "" {
		b, err := postgres.Open(ctx, url)
		if err != nil {
			return nil, err
		}
		return &Bundle{Container: b.Container, Sessions: b.Sessions, closer: b}, nil
	}
	b, err := sqlite.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return nil, err
	}
	return &Bundle{Container: b.Container, Sessions: b.Sessions, closer: b}, nil
}
