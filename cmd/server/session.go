package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"wacalls/internal/voip/call"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/wa"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

type Session struct {
	id   string
	name string
	mgr  *SessionManager
	log  *slog.Logger

	client *whatsmeow.Client
	cm     *call.CallManager

	mu          sync.Mutex
	bridge      *Bridge
	browserOpus media.Codec
	auth        AuthSnapshot
}

func newSession(mgr *SessionManager, id, name string, client *whatsmeow.Client) *Session {
	s := &Session{
		id:     id,
		name:   name,
		mgr:    mgr,
		log:    mgr.log.With("session", id),
		client: client,
		auth:   AuthSnapshot{State: "connecting"},
	}
	s.cm = call.NewCallManager(wa.NewSocket(client), s.log)
	s.wireCallManager()
	client.AddEventHandler(s.handleEvent)
	return s
}

func (s *Session) wireCallManager() {
	s.cm.OnIncoming = func(c *call.CallInfo) {
		s.mgr.broker.upsertCall(CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: "inbound", Peer: c.PeerJid,
			StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
		})
		s.mgr.broker.emitIncoming(s.id, c.CallID, c.PeerJid)
	}
	s.cm.OnStateChange = func(c *call.CallInfo) {
		dir := "outbound"
		if c.Direction == core.CallDirectionIncoming {
			dir = "inbound"
		}
		existing, _ := s.mgr.broker.getCall(c.CallID)
		rec := CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: dir, Peer: c.PeerJid,
			StartedAt: time.Now().UnixMilli(), Status: mapStatus(c.StateData.State),
		}
		if existing != nil {
			rec.Owner = existing.Owner
			rec.StartedAt = existing.StartedAt
		}
		// ConnectedAt is set once, when the call is answered (state ACTIVE);
		// the UI measures call duration from this, not from the dial time.
		if c.StateData.ConnectedAt != nil {
			ms := c.StateData.ConnectedAt.UnixMilli()
			rec.ConnectedAt = &ms
		}
		if c.IsEnded() {
			s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason))
			return
		}
		s.mgr.broker.upsertCall(rec)
	}
	s.cm.OnEnded = func(c *call.CallInfo) {
		s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason))
		s.closeBridge()
	}
	s.cm.OnPeerAudio = func(pcm16 []float32) {
		s.mu.Lock()
		br := s.bridge
		oc := s.browserOpus
		s.mu.Unlock()
		if br == nil || oc == nil {
			return
		}
		pcm48 := media.Upsample16to48(pcm16)
		opus, err := oc.Encode(pcm48)
		if err != nil || len(opus) == 0 {
			return
		}
		_ = br.WriteOpus(opus, 60*time.Millisecond)
	}
}

func (s *Session) handleEvent(rawEvt any) {
	ctx := context.Background()
	switch evt := rawEvt.(type) {
	case *events.Connected:
		if id := s.client.Store.ID; id != nil {
			_ = s.mgr.store.setJID(s.mgr.appCtx, s.id, id.String())
		}
		s.setAuth(AuthSnapshot{State: "open", Paired: true})
	case *events.LoggedOut:
		s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
	case *events.CallOffer:
		s.cm.HandleCallOffer(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallAccept:
		s.cm.HandleCallAccept(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallTransport:
		s.cm.HandleCallTransport(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallTerminate:
		s.cm.HandleCallTerminate(wrapCall(evt.From, evt.Data))
	case *events.CallReject:
		s.cm.HandleCallTerminate(wrapCall(evt.From, evt.Data))
	}
}

func (s *Session) connect(ctx context.Context) error {
	if s.client.Store.ID != nil {
		return s.client.Connect()
	}
	return s.startPairing(ctx)
}

func (s *Session) startPairing(ctx context.Context) error {
	qrChan, err := s.client.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := s.client.Connect(); err != nil {
		return err
	}
	go func() {
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				s.log.Info("scan the QR code to pair this session")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				s.setAuth(AuthSnapshot{State: "qr", QR: evt.Code})
				s.mgr.broker.emitSessionQR(s.id, evt.Code)
			case "success":
				if id := s.client.Store.ID; id != nil {
					_ = s.mgr.store.setJID(s.mgr.appCtx, s.id, id.String())
				}
				s.setAuth(AuthSnapshot{State: "open", Paired: true})
			case "timeout":
				s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
			}
		}
	}()
	return nil
}

func (s *Session) setAuth(a AuthSnapshot) {
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
	s.mgr.broker.emitAuthState(s.id, a)
	s.mgr.broker.emitSessionList(s.mgr.infos())
}

func (s *Session) info() SessionInfo {
	s.mu.Lock()
	a := s.auth
	s.mu.Unlock()
	jid := ""
	if id := s.client.Store.ID; id != nil {
		jid = id.String()
	}
	return SessionInfo{ID: s.id, Name: s.name, JID: jid, State: a.State, Paired: a.Paired || jid != ""}
}

func (s *Session) setBridge(b *Bridge, oc media.Codec) {
	s.mu.Lock()
	old := s.bridge
	oldOC := s.browserOpus
	s.bridge = b
	s.browserOpus = oc
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if oldOC != nil {
		oldOC.Close()
	}
}

func (s *Session) closeBridge() {
	s.mu.Lock()
	b := s.bridge
	oc := s.browserOpus
	s.bridge = nil
	s.browserOpus = nil
	s.mu.Unlock()
	if b != nil {
		b.Close()
	}
	if oc != nil {
		oc.Close()
	}
}

func (s *Session) replaceClient(client *whatsmeow.Client) {
	s.closeBridge()
	s.client.Disconnect()
	s.client = client
	s.cm = call.NewCallManager(wa.NewSocket(client), s.log)
	s.wireCallManager()
	client.AddEventHandler(s.handleEvent)
}

func (s *Session) shutdown() {
	s.closeBridge()
	s.client.Disconnect()
}

func mapStatus(state core.CallState) CallStatus {
	switch state {
	case core.CallStateActive:
		return StatusConnected
	case core.CallStateEnded:
		return StatusEnded
	case core.CallStateInitiating:
		return StatusStarting
	default:
		return StatusRinging
	}
}
