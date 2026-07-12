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

type CallRecordStore interface {
	Insert(ctx context.Context, r CallRecord) error
	List(ctx context.Context, sessionID string, limit int) ([]CallRecord, error)
	Prune(ctx context.Context, keep int) error
}
