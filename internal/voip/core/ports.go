package core

import "context"

type AudioCodec interface {
	Encode(pcm []float32) ([]byte, error)
	Decode(frame []byte) ([]float32, error)
	FrameSize() int
	SampleRate() int
	Close()
}

type AudioSink interface {
	FeedPCM(pcm []float32)
	OnPeerPCM(handler func(pcm []float32))
}

type Session struct {
	ID   string
	Name string
	JID  string
}

type SessionStore interface {
	List(ctx context.Context) ([]Session, error)
	Insert(ctx context.Context, id, name string) error
	SetJID(ctx context.Context, id, jid string) error
	Delete(ctx context.Context, id string) error
}

type CallRecord struct {
	CallID    string
	SessionID string
	Owner     *string
	Direction string
	Peer      string
	StartedAt int64
	EndedAt   int64
	EndReason string
}

type HistoryCursor struct {
	EndedAt int64
	CallID  string
}

type CallRecordStore interface {
	Insert(ctx context.Context, r CallRecord) error
	List(ctx context.Context, sessionID string, limit int, before HistoryCursor) ([]CallRecord, error)
	Prune(ctx context.Context, keep int) error
}

type ContactPhoto struct {
	SessionID string
	Jid       string
	URL       string
	PictureID string
	FetchedAt int64
}

type ContactPhotoStore interface {
	Get(ctx context.Context, sessionID, jid string) (ContactPhoto, bool, error)
	GetMany(ctx context.Context, sessionID string, jids []string) (map[string]ContactPhoto, error)
	Upsert(ctx context.Context, p ContactPhoto) error
}

type AdminCredential struct {
	Username     string
	PasswordHash string
}

type AuthStore interface {
	GetAdmin(ctx context.Context) (AdminCredential, bool, error)
	CreateAdmin(ctx context.Context, username, passwordHash string) error
	SetAdminPassword(ctx context.Context, passwordHash string) error
	CreateSession(ctx context.Context, tokenHash string, expiresAt int64) error
	SessionValid(ctx context.Context, tokenHash string, now int64) (bool, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteSessionsExcept(ctx context.Context, keepTokenHash string) error
	PurgeExpiredSessions(ctx context.Context, now int64) error
}
