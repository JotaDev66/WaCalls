package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"wacalls/internal/voip/core"
)

const historyCap = 10000

type CallStatus string

const (
	StatusStarting     CallStatus = "starting"
	StatusRinging      CallStatus = "ringing"
	StatusConnected    CallStatus = "connected"
	StatusReconnecting CallStatus = "reconnecting"
	StatusEnded        CallStatus = "ended"
)

type CallRecord struct {
	SessionID string     `json:"sessionId"`
	CallID    string     `json:"callId"`
	Owner     *string    `json:"owner"`
	Direction string     `json:"direction"`
	Peer      string     `json:"peer"`
	StartedAt int64      `json:"startedAt"`
	Status    CallStatus `json:"status"`
	EndedAt   *int64     `json:"endedAt,omitempty"`
	EndReason string     `json:"endReason,omitempty"`
}

type AuthSnapshot struct {
	State  string `json:"state"`
	Paired bool   `json:"paired"`
	QR     string `json:"qr,omitempty"`
}

type SessionInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	JID    string `json:"jid"`
	State  string `json:"state"`
	Paired bool   `json:"paired"`
}

type subscriber struct {
	clientID string
	ch       chan []byte
	kick     chan struct{}
	kickOnce sync.Once
}

type Broker struct {
	mu      sync.RWMutex
	subs    map[*subscriber]struct{}
	calls   map[string]*CallRecord
	records core.CallRecordStore
	log     *slog.Logger

	SnapshotFn func() []any
}

func NewBroker(records core.CallRecordStore, log *slog.Logger) *Broker {
	if log == nil {
		log = slog.Default()
	}
	return &Broker{
		subs:    map[*subscriber]struct{}{},
		calls:   map[string]*CallRecord{},
		records: records,
		log:     log,
	}
}

func (b *Broker) subscribe(clientID string) *subscriber {
	s := &subscriber{clientID: clientID, ch: make(chan []byte, 32), kick: make(chan struct{})}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Broker) unsubscribe(s *subscriber) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
	close(s.ch)
}

func (b *Broker) broadcast(ev any) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		select {
		case s.ch <- data:
		default:
			s.kickOnce.Do(func() {
				b.log.Warn("sse subscriber lagging, kicking for resync", "client_id", s.clientID)
				close(s.kick)
			})
		}
	}
}

func (b *Broker) emitAuthState(sessionID string, a AuthSnapshot) {
	b.broadcast(map[string]any{
		"type": "auth-state", "sessionId": sessionID,
		"paired": a.Paired, "state": a.State, "qr": a.QR,
	})
}

func (b *Broker) emitSessionList(sessions []SessionInfo) {
	b.broadcast(map[string]any{"type": "session-list", "sessions": sessions})
}

func (b *Broker) emitSessionQR(sessionID, qr string) {
	b.broadcast(map[string]any{"type": "session-qr", "sessionId": sessionID, "qr": qr})
}

func (b *Broker) upsertCall(r CallRecord) {
	b.mu.Lock()
	cp := r
	b.calls[r.CallID] = &cp
	b.mu.Unlock()
	b.broadcastCallList()
	b.broadcast(map[string]any{
		"type": "call-status", "sessionId": r.SessionID, "id": r.CallID, "owner": r.Owner,
		"status": r.Status, "peer": r.Peer, "startedAt": r.StartedAt,
	})
}

func (b *Broker) getCall(id string) (*CallRecord, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.calls[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

func (b *Broker) setOwner(id, owner string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.calls[id]
	if !ok {
		return false
	}
	if c.Owner != nil && *c.Owner != owner {
		return false
	}
	c.Owner = &owner
	return true
}

func (b *Broker) ownerActiveCall(owner string) string {
	if owner == "" {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for id, c := range b.calls {
		if c.Owner != nil && *c.Owner == owner && c.Status != StatusEnded {
			return id
		}
	}
	return ""
}

func (b *Broker) endCall(id, reason string) {
	b.mu.Lock()
	c, ok := b.calls[id]
	if !ok {
		b.mu.Unlock()
		return
	}
	now := time.Now().UnixMilli()
	c.Status = StatusEnded
	c.EndedAt = &now
	c.EndReason = reason
	ended := *c
	delete(b.calls, id)
	owner := c.Owner
	sessionID := c.SessionID
	b.mu.Unlock()

	b.broadcast(map[string]any{
		"type": "call-ended", "sessionId": sessionID, "id": id, "owner": owner, "reason": reason, "endedAt": now,
	})
	b.broadcastCallList()
	b.persist(ended)
}

func (b *Broker) persist(rec CallRecord) {
	if b.records == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var endedAt int64
	if rec.EndedAt != nil {
		endedAt = *rec.EndedAt
	}
	cr := core.CallRecord{
		CallID: rec.CallID, SessionID: rec.SessionID, Owner: rec.Owner,
		Direction: rec.Direction, Peer: rec.Peer,
		StartedAt: rec.StartedAt, EndedAt: endedAt, EndReason: rec.EndReason,
	}
	if err := b.records.Insert(ctx, cr); err != nil {
		b.log.Error("persist call record", "call_id", rec.CallID, "err", err)
		return
	}
	if err := b.records.Prune(ctx, historyCap); err != nil {
		b.log.Error("prune call records", "err", err)
	}
}

func (b *Broker) callList() []CallRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	list := make([]CallRecord, 0, len(b.calls))
	for _, c := range b.calls {
		list = append(list, *c)
	}
	return list
}

func (b *Broker) sessionCalls(sid string) []CallRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	list := []CallRecord{}
	for _, c := range b.calls {
		if c.SessionID == sid {
			list = append(list, *c)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].StartedAt != list[j].StartedAt {
			return list[i].StartedAt < list[j].StartedAt
		}
		return list[i].CallID < list[j].CallID
	})
	return list
}

func (b *Broker) broadcastCallList() {
	b.broadcast(map[string]any{"type": "call-list", "calls": b.callList()})
}

func (b *Broker) emitIncoming(sessionID, id, peer string) {
	b.broadcast(map[string]any{
		"type": "incoming", "sessionId": sessionID, "id": id, "peer": peer,
		"offeredAt": time.Now().UnixMilli(),
	})
}

func (b *Broker) emitIncomingClaimed(sessionID, id, owner string) {
	b.broadcast(map[string]any{"type": "incoming-claimed", "sessionId": sessionID, "id": id, "owner": owner})
}

func (b *Broker) historyRows(ctx context.Context, sessionID string, limit int) ([]CallRecord, error) {
	if b.records == nil {
		return []CallRecord{}, nil
	}
	recs, err := b.records.List(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	rows := make([]CallRecord, 0, len(recs))
	for _, r := range recs {
		endedAt := r.EndedAt
		rows = append(rows, CallRecord{
			SessionID: r.SessionID, CallID: r.CallID, Owner: r.Owner, Direction: r.Direction,
			Peer: r.Peer, StartedAt: r.StartedAt, Status: StatusEnded,
			EndedAt: &endedAt, EndReason: r.EndReason,
		})
	}
	return rows, nil
}

func (b *Broker) serveSSE(w http.ResponseWriter, r *http.Request, clientID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sub := b.subscribe(clientID)
	defer b.unsubscribe(sub)

	if b.SnapshotFn != nil {
		for _, ev := range b.SnapshotFn() {
			writeSSE(w, flusher, ev)
		}
	}
	writeSSE(w, flusher, map[string]any{"type": "call-list", "calls": b.callList()})

	keepalive := time.NewTicker(10 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.kick:
			return
		case data := <-sub.ch:
			if _, err := w.Write(append(append([]byte("data: "), data...), '\n', '\n')); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			// A data event instead of an SSE comment so the client can track
			// stream liveness and force a reconnect when pings stop arriving.
			writeSSE(w, flusher, map[string]any{"type": "ping"})
		}
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, ev any) {
	data, _ := json.Marshal(ev)
	_, _ = w.Write(append(append([]byte("data: "), data...), '\n', '\n'))
	f.Flush()
}
