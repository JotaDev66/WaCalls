package app

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"wacalls/internal/telemetry"
	"wacalls/internal/voip/call"
	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/codec/opus"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/extension/audio"
	"wacalls/internal/wa"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Session struct {
	id   string
	name string
	mgr  *SessionManager
	log  *slog.Logger

	client *whatsmeow.Client
	calls  *call.Client

	bridgeMu sync.Mutex
	bridges  map[string]*Bridge

	mu   sync.Mutex
	auth AuthSnapshot
}

func newSession(mgr *SessionManager, id, name string, client *whatsmeow.Client) *Session {
	s := &Session{
		id:      id,
		name:    name,
		mgr:     mgr,
		log:     mgr.log.With("session", id),
		client:  client,
		auth:    AuthSnapshot{State: "connecting"},
		bridges: map[string]*Bridge{},
	}
	s.calls = call.NewClient(wa.NewSocket(client), s.log, s.makeExtensions, mgr.maxCalls, s.wireCall, mgr.newObserver)
	client.AddEventHandler(s.handleEvent)
	return s
}

func (s *Session) makeExtensions() []engine.Extension {
	var exts []engine.Extension
	if codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions); err == nil {
		exts = append(exts, audio.New(opus.WithFallback(codec)))
	} else {
		s.log.Warn("MLow codec unavailable; call runs without audio", "err", err)
	}
	return exts
}

func (s *Session) wireCall(callID string, cm *call.CallManager) {
	cm.OnIncoming = func(c *call.CallInfo) {
		s.mgr.broker.upsertCall(CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: "inbound", Peer: c.PeerJid,
			StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
		})
		s.mgr.broker.emitIncoming(s.id, c.CallID, c.PeerJid)
		s.mgr.tracer.StartCall(c.CallID, telemetry.CallAttrs{Session: s.id, Peer: c.PeerJid, Direction: "inbound"})
	}
	cm.OnStateChange = func(c *call.CallInfo) {
		if c.IsEnded() {
			s.mgr.tracer.EndCall(c.CallID, endResult(c), string(c.StateData.EndReason), endDuration(c))
			s.removeCall(c.CallID)
			s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason))
			return
		}
		dir := "outbound"
		if c.Direction == core.CallDirectionIncoming {
			dir = "inbound"
		}
		existing, _ := s.mgr.broker.getCall(c.CallID)
		if existing == nil {
			s.mgr.tracer.StartCall(c.CallID, telemetry.CallAttrs{Session: s.id, Peer: c.PeerJid, Direction: dir})
		}
		if mapStatus(c.StateData.State) == StatusConnected && c.StateData.ConnectedAt != nil {
			s.mgr.tracer.MarkActive(c.CallID, c.StateData.ConnectedAt.Sub(c.CreatedAt))
		}
		rec := CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: dir, Peer: c.PeerJid,
			StartedAt: time.Now().UnixMilli(), Status: mapStatus(c.StateData.State),
		}
		if existing != nil {
			rec.Owner = existing.Owner
			rec.StartedAt = existing.StartedAt
		}
		s.mgr.broker.upsertCall(rec)
	}
	cm.OnEnded = func(c *call.CallInfo) {
		s.mgr.tracer.EndCall(c.CallID, endResult(c), string(c.StateData.EndReason), endDuration(c))
		s.removeCall(c.CallID)
		s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason))
	}
	cm.OnPeerAudio = func(pcm16 []float32) {
		if b := s.getBridge(callID); b != nil {
			_ = b.WritePCM(pcm16)
		}
	}
	cm.OnQuality = func(callID string, q core.CallQuality) {
		s.mgr.broker.emitCallQuality(s.id, callID, q)
	}
	cm.OnMark = func(callID string, mark string, elapsedMs int64) {
		s.mgr.broker.emitCallMark(s.id, callID, mark, elapsedMs)
	}
}

func (s *Session) startOutgoing(ctx context.Context, peer types.JID) (string, error) {
	return s.calls.StartCall(ctx, peer)
}

func (s *Session) callFor(callID string) (*call.CallManager, bool) {
	return s.calls.Get(callID)
}

func (s *Session) callCount() int {
	return s.calls.Count()
}

func (s *Session) handleEvent(rawEvt any) {
	ctx := context.Background()
	switch evt := rawEvt.(type) {
	case *events.Connected:
		if id := s.client.Store.ID; id != nil {
			_ = s.mgr.store.SetJID(s.mgr.appCtx, s.id, id.String())
		}
		s.setAuth(AuthSnapshot{State: "open", Paired: true})
	case *events.LoggedOut:
		s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
	case *events.CallOffer:
		s.calls.HandleOffer(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallAccept:
		s.calls.HandleAccept(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallTransport:
		s.calls.HandleTransport(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallRelayLatency:
		s.calls.HandleRelayLatency(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *events.CallTerminate:
		s.calls.HandleTerminate(wrapCall(evt.From, evt.Data))
	case *events.CallReject:
		s.calls.HandleTerminate(wrapCall(evt.From, evt.Data))
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
					_ = s.mgr.store.SetJID(s.mgr.appCtx, s.id, id.String())
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

func (s *Session) getBridge(callID string) *Bridge {
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	return s.bridges[callID]
}

func (s *Session) setBridge(callID string, b *Bridge) {
	s.bridgeMu.Lock()
	old := s.bridges[callID]
	if _, live := s.calls.Get(callID); !live {
		s.bridgeMu.Unlock()
		b.Close()
		return
	}
	s.bridges[callID] = b
	s.bridgeMu.Unlock()
	if old != nil {
		old.Close()
	}
}

func (s *Session) removeCall(callID string) {
	s.bridgeMu.Lock()
	b := s.bridges[callID]
	delete(s.bridges, callID)
	s.bridgeMu.Unlock()
	if b != nil {
		b.Close()
	}
	s.calls.Remove(callID)
}

func (s *Session) terminateCall(callID string, reason core.EndCallReason) {
	_ = s.calls.EndCall(context.Background(), callID, reason)
}

func (s *Session) teardownAllCalls() {
	for _, cm := range s.calls.Drain() {
		_ = cm.EndCall(context.Background(), core.EndCallReasonUserEnded)
	}
	s.bridgeMu.Lock()
	bridges := s.bridges
	s.bridges = map[string]*Bridge{}
	s.bridgeMu.Unlock()
	for _, b := range bridges {
		if b != nil {
			b.Close()
		}
	}
}

func (s *Session) replaceClient(client *whatsmeow.Client) {
	s.teardownAllCalls()
	s.client.Disconnect()
	s.client = client
	client.AddEventHandler(s.handleEvent)
}

func (s *Session) shutdown() {
	s.teardownAllCalls()
	s.client.Disconnect()
}

func endResult(c *call.CallInfo) string {
	if c.StateData.ConnectedAt != nil {
		return "completed"
	}
	return "failed"
}

func endDuration(c *call.CallInfo) time.Duration {
	if c.StateData.EndedAt != nil {
		return c.StateData.EndedAt.Sub(c.CreatedAt)
	}
	return 0
}

func mapStatus(state core.CallState) CallStatus {
	switch state {
	case core.CallStateActive:
		return StatusConnected
	case core.CallStateReconnecting:
		return StatusReconnecting
	case core.CallStateEnded:
		return StatusEnded
	case core.CallStateInitiating:
		return StatusStarting
	default:
		return StatusRinging
	}
}
