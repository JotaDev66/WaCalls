package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"wacalls/internal/store/migrate"
	"wacalls/internal/voip/core"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Bundle struct {
	Container *sqlstore.Container
	Sessions  core.SessionStore
	Calls     core.CallRecordStore
	Photos    core.ContactPhotoStore
	Auth      core.AuthStore
	db        *sql.DB
}

var migrations = [][]string{
	{`CREATE TABLE IF NOT EXISTS sessions (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		jid  TEXT,
		seq  BIGSERIAL
	)`},
	{`CREATE TABLE call_records (
		call_id    TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		owner      TEXT,
		direction  TEXT NOT NULL,
		peer       TEXT NOT NULL,
		started_at BIGINT NOT NULL,
		ended_at   BIGINT NOT NULL,
		end_reason TEXT NOT NULL DEFAULT ''
	)`,
		`CREATE INDEX idx_call_records_session_ended ON call_records (session_id, ended_at DESC)`},
	{`CREATE TABLE contact_photos (
		session_id TEXT NOT NULL,
		jid        TEXT NOT NULL,
		url        TEXT NOT NULL,
		picture_id TEXT NOT NULL DEFAULT '',
		fetched_at BIGINT NOT NULL,
		PRIMARY KEY (session_id, jid)
	)`},
	{`CREATE TABLE auth_user (
		id            SMALLINT PRIMARY KEY CHECK (id = 1),
		username      TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		updated_at    BIGINT NOT NULL
	)`,
		`CREATE TABLE auth_session (
		token_hash TEXT PRIMARY KEY,
		expires_at BIGINT NOT NULL,
		created_at BIGINT NOT NULL
	)`,
		`CREATE INDEX idx_auth_session_expires ON auth_session (expires_at)`},
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
		_ = db.Close()
		return nil, err
	}
	container := sqlstore.NewWithDB(db, "postgres", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate.Apply(ctx, db, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Bundle{Container: container, Sessions: &sessionStore{db: db}, Calls: &callRecordStore{db: db}, Photos: &contactPhotoStore{db: db}, Auth: &authStore{db: db}, db: db}, nil
}

func (b *Bundle) Close() error { return b.db.Close() }

type sessionStore struct{ db *sql.DB }

func (s *sessionStore) List(ctx context.Context) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(jid, '') FROM sessions ORDER BY seq`)
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

type callRecordStore struct{ db *sql.DB }

func (s *callRecordStore) Insert(ctx context.Context, r core.CallRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_records
		(call_id, session_id, owner, direction, peer, started_at, ended_at, end_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (call_id) DO NOTHING`,
		r.CallID, r.SessionID, r.Owner, r.Direction, r.Peer, r.StartedAt, r.EndedAt, r.EndReason)
	return err
}

func (s *callRecordStore) List(ctx context.Context, sessionID string, limit int, before core.HistoryCursor) ([]core.CallRecord, error) {
	q := `SELECT call_id, session_id, owner, direction, peer, started_at, ended_at, end_reason FROM call_records`
	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if sessionID != "" {
		conds = append(conds, `session_id = `+arg(sessionID))
	}
	if before != (core.HistoryCursor{}) {
		e := arg(before.EndedAt)
		c := arg(before.CallID)
		conds = append(conds, `(ended_at < `+e+` OR (ended_at = `+e+` AND call_id < `+c+`))`)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY ended_at DESC, call_id DESC LIMIT ` + arg(limit)
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
		SELECT call_id FROM call_records ORDER BY ended_at DESC LIMIT $1)`, keep)
	return err
}

var _ core.CallRecordStore = (*callRecordStore)(nil)

type contactPhotoStore struct{ db *sql.DB }

func (s *contactPhotoStore) Upsert(ctx context.Context, p core.ContactPhoto) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO contact_photos
		(session_id, jid, url, picture_id, fetched_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (session_id, jid) DO UPDATE SET
			url = excluded.url, picture_id = excluded.picture_id, fetched_at = excluded.fetched_at`,
		p.SessionID, p.Jid, p.URL, p.PictureID, p.FetchedAt)
	return err
}

func (s *contactPhotoStore) Get(ctx context.Context, sessionID, jid string) (core.ContactPhoto, bool, error) {
	var p core.ContactPhoto
	err := s.db.QueryRowContext(ctx,
		`SELECT session_id, jid, url, picture_id, fetched_at FROM contact_photos WHERE session_id = $1 AND jid = $2`,
		sessionID, jid).Scan(&p.SessionID, &p.Jid, &p.URL, &p.PictureID, &p.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return core.ContactPhoto{}, false, nil
	}
	if err != nil {
		return core.ContactPhoto{}, false, err
	}
	return p, true, nil
}

func (s *contactPhotoStore) GetMany(ctx context.Context, sessionID string, jids []string) (map[string]core.ContactPhoto, error) {
	out := map[string]core.ContactPhoto{}
	if len(jids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(jids)+1)
	args = append(args, sessionID)
	ph := make([]string, len(jids))
	for i, j := range jids {
		args = append(args, j)
		ph[i] = "$" + strconv.Itoa(i+2)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, jid, url, picture_id, fetched_at FROM contact_photos WHERE session_id = $1 AND jid IN (`+strings.Join(ph, ",")+`)`,
		args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p core.ContactPhoto
		if err := rows.Scan(&p.SessionID, &p.Jid, &p.URL, &p.PictureID, &p.FetchedAt); err != nil {
			return nil, err
		}
		out[p.Jid] = p
	}
	return out, rows.Err()
}

var _ core.ContactPhotoStore = (*contactPhotoStore)(nil)

type authStore struct{ db *sql.DB }

func (s *authStore) GetAdmin(ctx context.Context) (core.AdminCredential, bool, error) {
	var c core.AdminCredential
	err := s.db.QueryRowContext(ctx, `SELECT username, password_hash FROM auth_user WHERE id = 1`).
		Scan(&c.Username, &c.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return core.AdminCredential{}, false, nil
	}
	if err != nil {
		return core.AdminCredential{}, false, err
	}
	return c, true, nil
}

func (s *authStore) CreateAdmin(ctx context.Context, username, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_user (id, username, password_hash, updated_at) VALUES (1, $1, $2, EXTRACT(epoch FROM now())::bigint)`,
		username, passwordHash)
	return err
}

func (s *authStore) SetAdminPassword(ctx context.Context, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE auth_user SET password_hash = $1, updated_at = EXTRACT(epoch FROM now())::bigint WHERE id = 1`, passwordHash)
	return err
}

func (s *authStore) CreateSession(ctx context.Context, tokenHash string, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_session (token_hash, expires_at, created_at) VALUES ($1, $2, EXTRACT(epoch FROM now())::bigint)`,
		tokenHash, expiresAt)
	return err
}

func (s *authStore) SessionValid(ctx context.Context, tokenHash string, now int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM auth_session WHERE token_hash = $1 AND expires_at > $2`, tokenHash, now).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *authStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_session WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *authStore) DeleteSessionsExcept(ctx context.Context, keepTokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_session WHERE token_hash != $1`, keepTokenHash)
	return err
}

func (s *authStore) PurgeExpiredSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_session WHERE expires_at <= $1`, now)
	return err
}

var _ core.AuthStore = (*authStore)(nil)
