package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"wacalls/internal/voip/core"
)

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
	mu       sync.RWMutex
	subs     map[*subscriber]struct{}
	calls    map[string]*CallRecord
	records  core.CallRecordStore
	webhooks *webhookDispatcher
	log      *slog.Logger

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

func (b *Broker) emitIncoming(sessionID, id, peer, peerName, peerPhotoURL string) {
	b.broadcast(map[string]any{
		"type": "incoming", "sessionId": sessionID, "id": id, "peer": peer,
		"peerName": peerName, "peerPhotoUrl": peerPhotoURL,
		"offeredAt": time.Now().UnixMilli(),
	})
}

func (b *Broker) emitIncomingClaimed(sessionID, id, owner string) {
	b.broadcast(map[string]any{"type": "incoming-claimed", "sessionId": sessionID, "id": id, "owner": owner})
}

// emitCallQuality broadcasts a live per-call reception-quality sample (RTT, jitter, loss) derived
// from inbound RTCP. It is a transient live-only signal: it never touches CallRecord, persistence,
// or webhooks, so the client's call card can render it without polluting the history contract.
func (b *Broker) emitCallQuality(sessionID, callID string, q core.CallQuality) {
	b.broadcast(map[string]any{
		"type": "call-quality", "sessionId": sessionID, "id": callID,
		"rttMs": q.RttMs, "jitterMs": q.JitterMs, "lossFraction": q.LossFraction, "hasRtt": q.HasRtt,
	})
}

// emitCallMark broadcasts a connection-setup phase mark (STUN/ICE/DTLS/SCTP/first-packet) with its
// elapsed time since call start. Like call-quality it is a transient live-only signal, never
// persisted, feeding the client's connection timeline.
func (b *Broker) emitCallMark(sessionID, callID, mark string, elapsedMs int64) {
	b.broadcast(map[string]any{
		"type": "call-mark", "sessionId": sessionID, "id": callID,
		"mark": mark, "elapsedMs": elapsedMs,
	})
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
