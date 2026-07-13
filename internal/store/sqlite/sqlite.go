package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"wacalls/internal/store/migrate"
	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

type Bundle struct {
	Container *sqlstore.Container
	Sessions  core.SessionStore
	Calls     core.CallRecordStore
	db        *sql.DB
}

var migrations = [][]string{
	{`CREATE TABLE IF NOT EXISTS sessions (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		jid  TEXT
	)`},
	{`CREATE TABLE call_records (
		call_id    TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		owner      TEXT,
		direction  TEXT NOT NULL,
		peer       TEXT NOT NULL,
		started_at INTEGER NOT NULL,
		ended_at   INTEGER NOT NULL,
		end_reason TEXT NOT NULL DEFAULT ''
	)`,
		`CREATE INDEX idx_call_records_session_ended ON call_records (session_id, ended_at DESC)`},
}

func Open(ctx context.Context, path string) (*Bundle, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		return nil, err
	}
	if err := migrate.Apply(ctx, db, migrations); err != nil {
		return nil, err
	}
	return &Bundle{Container: container, Sessions: &sessionStore{db: db}, Calls: &callRecordStore{db: db}, db: db}, nil
}

func (b *Bundle) Close() error {
	return b.db.Close()
}

type sessionStore struct{ db *sql.DB }

func (s *sessionStore) List(ctx context.Context) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(jid, '') FROM sessions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, name, jid) VALUES (?, ?, NULL)`, id, name)
	return err
}

func (s *sessionStore) SetJID(ctx context.Context, id, jid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET jid = ? WHERE id = ?`, jid, id)
	return err
}

func (s *sessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

var _ core.SessionStore = (*sessionStore)(nil)

type callRecordStore struct{ db *sql.DB }

func (s *callRecordStore) Insert(ctx context.Context, r core.CallRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_records
		(call_id, session_id, owner, direction, peer, started_at, ended_at, end_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (call_id) DO NOTHING`,
		r.CallID, r.SessionID, r.Owner, r.Direction, r.Peer, r.StartedAt, r.EndedAt, r.EndReason)
	return err
}

func (s *callRecordStore) List(ctx context.Context, sessionID string, limit int, before core.HistoryCursor) ([]core.CallRecord, error) {
	q := `SELECT call_id, session_id, owner, direction, peer, started_at, ended_at, end_reason FROM call_records`
	var conds []string
	var args []any
	if sessionID != "" {
		conds = append(conds, `session_id = ?`)
		args = append(args, sessionID)
	}
	if before != (core.HistoryCursor{}) {
		conds = append(conds, `(ended_at < ? OR (ended_at = ? AND call_id < ?))`)
		args = append(args, before.EndedAt, before.EndedAt, before.CallID)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY ended_at DESC, call_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []core.CallRecord
	for rows.Next() {
		var r core.CallRecord
		if err := rows.Scan(&r.CallID, &r.SessionID, &r.Owner, &r.Direction, &r.Peer, &r.StartedAt, &r.EndedAt, &r.EndReason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *callRecordStore) Prune(ctx context.Context, keep int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM call_records WHERE call_id NOT IN (
		SELECT call_id FROM call_records ORDER BY ended_at DESC LIMIT ?)`, keep)
	return err
}

var _ core.CallRecordStore = (*callRecordStore)(nil)
