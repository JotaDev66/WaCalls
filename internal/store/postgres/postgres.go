package postgres

import (
	"context"
	"database/sql"
	"time"

	"wacalls/internal/voip/core"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Bundle struct {
	Container *sqlstore.Container
	Sessions  core.SessionStore
	db        *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*Bundle, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	container := sqlstore.NewWithDB(db, "postgres", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		db.Close()
		return nil, err
	}
	sessions, err := newSessionStore(ctx, db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Bundle{Container: container, Sessions: sessions, db: db}, nil
}

func (b *Bundle) Close() error { return b.db.Close() }

type sessionStore struct{ db *sql.DB }

func newSessionStore(ctx context.Context, db *sql.DB) (*sessionStore, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sessions (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		jid  TEXT,
		seq  BIGSERIAL
	)`)
	if err != nil {
		return nil, err
	}
	return &sessionStore{db: db}, nil
}

func (s *sessionStore) List(ctx context.Context) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(jid, '') FROM sessions ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		var r core.Session
		if err := rows.Scan(&r.ID, &r.Name, &r.JID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sessionStore) Insert(ctx context.Context, id, name string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, name, jid) VALUES ($1, $2, NULL)`, id, name)
	return err
}

func (s *sessionStore) SetJID(ctx context.Context, id, jid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET jid = $1 WHERE id = $2`, jid, id)
	return err
}

func (s *sessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

var _ core.SessionStore = (*sessionStore)(nil)
