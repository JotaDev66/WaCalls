package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
)

type sessionRow struct {
	ID      string
	Name    string
	JID     string
	APIKey  string
	SIPUser string
	SIPPass string
	SIPURL  string
}

type sessionStore struct{ db *sql.DB }

func newSessionStore(ctx context.Context, db *sql.DB) (*sessionStore, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sessions (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		jid  TEXT,
		api_key TEXT,
		sip_user TEXT,
		sip_pass TEXT,
		sip_url TEXT
	)`)
	if err != nil {
		return nil, err
	}
	// Migrate database quietly
	_, _ = db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN api_key TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN sip_user TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN sip_pass TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN sip_url TEXT`)
	return &sessionStore{db: db}, nil
}

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *sessionStore) list(ctx context.Context) ([]sessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(jid, ''), COALESCE(api_key, ''), COALESCE(sip_user, ''), COALESCE(sip_pass, ''), COALESCE(sip_url, '') FROM sessions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	var out []sessionRow
	var updates []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.ID, &r.Name, &r.JID, &r.APIKey, &r.SIPUser, &r.SIPPass, &r.SIPURL); err != nil {
			rows.Close()
			return nil, err
		}
		if r.APIKey == "" {
			r.APIKey = "wac_" + generateRandomString(16)
			r.SIPUser = "sip_" + r.ID[:8]
			r.SIPPass = generateRandomString(12)
			r.SIPURL = ""
			updates = append(updates, r)
		}
		out = append(out, r)
	}
	err = rows.Err()
	rows.Close()

	for _, r := range updates {
		s.db.ExecContext(ctx, "UPDATE sessions SET api_key = ?, sip_user = ?, sip_pass = ?, sip_url = ? WHERE id = ?", r.APIKey, r.SIPUser, r.SIPPass, r.SIPURL, r.ID)
	}
	return out, err
}

func (s *sessionStore) insert(ctx context.Context, id, name string) (string, string, string, string, error) {
	apiKey := "wac_" + generateRandomString(16)
	sipUser := "sip_" + id[:8]
	sipPass := generateRandomString(12)
	sipURL := "127.0.0.1:5060" // Direct host

	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, name, jid, api_key, sip_user, sip_pass, sip_url) VALUES (?, ?, NULL, ?, ?, ?, ?)`,
		id, name, apiKey, sipUser, sipPass, sipURL)
	return apiKey, sipUser, sipPass, sipURL, err
}

func (s *sessionStore) setJID(ctx context.Context, id, jid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET jid = ? WHERE id = ?`, jid, id)
	return err
}

func (s *sessionStore) delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}
