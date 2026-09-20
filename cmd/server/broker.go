package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type CallStatus string

const (
	StatusStarting     CallStatus = "starting"
	StatusRinging      CallStatus = "ringing"
	StatusConnected    CallStatus = "connected"
	StatusHeld         CallStatus = "held"
	StatusTransferring CallStatus = "transferring"
	StatusEnded        CallStatus = "ended"
)

type CallRecord struct {
	SessionID string     `json:"sessionId"`
	CallID    string     `json:"callId"`
	Owner     *string    `json:"owner"`
	Direction string     `json:"direction"`
	Peer      string     `json:"peer"`
	Media     string     `json:"media,omitempty"` // "audio" (default) or "video"
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
}

type Broker struct {
	mu       sync.RWMutex
	subs     map[*subscriber]struct{}
	calls    map[string]*CallRecord
	reserved map[string]struct{} // owner -> in-flight outbound-call reservation
	history  []CallRecord

	SnapshotFn func() []any
}

func NewBroker() *Broker {
	return &Broker{
		subs:     map[*subscriber]struct{}{},
		calls:    map[string]*CallRecord{},
		reserved: map[string]struct{}{},
	}
}

func (b *Broker) subscribe(clientID string) *subscriber {
	s := &subscriber{clientID: clientID, ch: make(chan []byte, 32)}
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
		"status": r.Status, "peer": r.Peer, "media": r.Media, "startedAt": r.StartedAt,
	})
}

// setStatus atualiza só o status de um registro de chamada existente e
// transmite do mesmo jeito que upsertCall faz, sem o chamador precisar
// reconstruir o resto do registro. Devolve false se a chamada é desconhecida.
func (b *Broker) setStatus(id string, status CallStatus) (CallRecord, bool) {
	b.mu.Lock()
	c, ok := b.calls[id]
	if !ok {
		b.mu.Unlock()
		return CallRecord{}, false
	}
	c.Status = status
	cp := *c
	b.mu.Unlock()
	b.broadcastCallList()
	b.broadcast(map[string]any{
		"type": "call-status", "sessionId": cp.SessionID, "id": cp.CallID, "owner": cp.Owner,
		"status": cp.Status, "peer": cp.Peer, "media": cp.Media, "startedAt": cp.StartedAt,
	})
	return cp, true
}

// setCallMedia atualiza só o campo Media de um registro existente e
// retransmite via SSE (call-status), sem mexer no resto do registro — usado
// pelo upgrade/downgrade de vídeo (tanto o nosso quanto o aceite automático
// de um upgrade pedido pelo peer) pra refletir na UI sem exigir um novo GET.
func (b *Broker) setCallMedia(callID, kind string) {
	rec, ok := b.getCall(callID)
	if !ok {
		return
	}
	rec.Media = kind
	b.upsertCall(*rec)
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

// forceSetOwner reatribui o dono de uma chamada incondicionalmente,
// independente de quem a detém atualmente. Usado só pelo pickup, onde outro
// agente assumir é o objetivo inteiro — ao contrário de setOwner, nunca recusa.
func (b *Broker) forceSetOwner(id, owner string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.calls[id]
	if !ok {
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

// tryReserveOwner atomically checks that owner has no active call and no
// in-flight reservation, and if so reserves owner for an outbound call
// being started. Call releaseReservation(owner) exactly once, however the
// attempt ends (success or failure).
func (b *Broker) tryReserveOwner(owner string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if owner == "" {
		return false
	}
	if _, reserved := b.reserved[owner]; reserved {
		return false
	}
	for _, c := range b.calls {
		if c.Owner != nil && *c.Owner == owner && c.Status != StatusEnded {
			return false
		}
	}
	b.reserved[owner] = struct{}{}
	return true
}

func (b *Broker) releaseReservation(owner string) {
	b.mu.Lock()
	delete(b.reserved, owner)
	b.mu.Unlock()
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
	b.history = append(b.history, ended)
	owner := c.Owner
	sessionID := c.SessionID
	b.mu.Unlock()

	b.broadcast(map[string]any{
		"type": "call-ended", "sessionId": sessionID, "id": id, "owner": owner, "reason": reason, "endedAt": now,
	})
	b.broadcastCallList()
}

func (b *Broker) broadcastCallList() {
	b.mu.RLock()
	list := make([]CallRecord, 0, len(b.calls))
	for _, c := range b.calls {
		list = append(list, *c)
	}
	b.mu.RUnlock()
	b.broadcast(map[string]any{"type": "call-list", "calls": list})
}

func (b *Broker) emitIncoming(sessionID, id, peer, media string) {
	b.broadcast(map[string]any{
		"type": "incoming", "sessionId": sessionID, "id": id, "peer": peer, "media": media,
		"offeredAt": time.Now().UnixMilli(),
	})
}

func (b *Broker) emitIncomingClaimed(sessionID, id, owner string) {
	b.broadcast(map[string]any{"type": "incoming-claimed", "sessionId": sessionID, "id": id, "owner": owner})
}

// emitReaction transmite uma reação de chamada (emoji ou texto livre, pelo
// stream de app-data da própria chamada) de um participante. sender fica
// vazio pra uma reação nossa enviada, ou o JID do peer pra uma recebida.
func (b *Broker) emitReaction(sessionID, callID, sender, text string) {
	b.broadcast(map[string]any{
		"type": "call-reaction", "sessionId": sessionID, "id": callID,
		"sender": sender, "text": text, "at": time.Now().UnixMilli(),
	})
}

// emitHandRaise transmite o estado persistente de "mão levantada" de um participante.
func (b *Broker) emitHandRaise(sessionID, callID, participant string, raised bool) {
	b.broadcast(map[string]any{
		"type": "call-hand", "sessionId": sessionID, "id": callID,
		"participant": participant, "raised": raised,
	})
}

// emitPeerVideoState transmite uma mudança no estado de vídeo do peer (câmera
// ligada/desligada, ou um upgrade de áudio pra vídeo pedido por ele) — vem de
// um stanza <video> recebido no meio da chamada.
func (b *Broker) emitPeerVideoState(sessionID, callID string, active, upgrade bool, orientation int) {
	b.broadcast(map[string]any{
		"type": "call-peer-video", "sessionId": sessionID, "id": callID,
		"active": active, "upgrade": upgrade, "orientation": orientation,
	})
}

// emitCallHeld/emitCallUnheld transmitem as transições de estado de hold.
func (b *Broker) emitCallHeld(sessionID, callID string) {
	b.broadcast(map[string]any{"type": "call-held", "sessionId": sessionID, "id": callID})
}

func (b *Broker) emitCallUnheld(sessionID, callID string) {
	b.broadcast(map[string]any{"type": "call-unheld", "sessionId": sessionID, "id": callID})
}

// emitCallPickedUp transmite que o bridge WebRTC de uma chamada foi fechado
// pra outro agente poder reconectar (ver doPickup).
func (b *Broker) emitCallPickedUp(sessionID, callID string) {
	b.broadcast(map[string]any{"type": "call-picked-up", "sessionId": sessionID, "id": callID})
}

// emitTransferStarted/Completed/Failed transmitem o ciclo de vida de uma
// tentativa de transferência cega (ver doTransfer).
func (b *Broker) emitTransferStarted(sessionID, originalCallID, transferCallID string) {
	b.broadcast(map[string]any{
		"type": "transfer-started", "sessionId": sessionID,
		"original_call_id": originalCallID, "transfer_call_id": transferCallID,
	})
}

func (b *Broker) emitTransferCompleted(sessionID, originalCallID, transferCallID string) {
	b.broadcast(map[string]any{
		"type": "transfer-completed", "sessionId": sessionID,
		"original_call_id": originalCallID, "transfer_call_id": transferCallID,
	})
}

func (b *Broker) emitTransferFailed(sessionID, originalCallID, transferCallID, reason string) {
	b.broadcast(map[string]any{
		"type": "transfer-failed", "sessionId": sessionID,
		"original_call_id": originalCallID, "transfer_call_id": transferCallID, "reason": reason,
	})
}

func (b *Broker) historyRows(sessionID string, limit int) []CallRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	rows := make([]CallRecord, 0, limit)
	for i := len(b.history) - 1; i >= 0 && len(rows) < limit; i-- {
		if sessionID == "" || b.history[i].SessionID == sessionID {
			rows = append(rows, b.history[i])
		}
	}
	return rows
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
	b.broadcastCallList()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-sub.ch:
			if _, err := w.Write(append(append([]byte("data: "), data...), '\n', '\n')); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, ev any) {
	data, _ := json.Marshal(ev)
	w.Write(append(append([]byte("data: "), data...), '\n', '\n'))
	f.Flush()
}
