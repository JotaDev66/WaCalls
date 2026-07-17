package session

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"wacalls/internal/app/events"
	"wacalls/internal/telemetry"
	"wacalls/internal/voip/call"
	"wacalls/internal/voip/codec/mlow"
	"wacalls/internal/voip/codec/nativemlow"
	"wacalls/internal/voip/codec/opus"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/extension/audio"
	"wacalls/internal/wa"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waevents "go.mau.fi/whatsmeow/types/events"
)

type Session struct {
	id   string
	name string
	mgr  *Manager
	log  *slog.Logger

	client *whatsmeow.Client
	calls  *call.Client

	bridgeMu sync.Mutex
	bridges  map[string]*Bridge
	grace    *graceKeeper

	mu   sync.Mutex
	auth events.AuthSnapshot
}

// browserGraceWindow is how long a call is held after its browser leg drops (e.g. a
// page refresh) before it is terminated, giving a reloaded page time to re-attach.
const browserGraceWindow = 30 * time.Second

func newSession(mgr *Manager, id, name string, client *whatsmeow.Client) *Session {
	s := &Session{
		id:      id,
		name:    name,
		mgr:     mgr,
		log:     mgr.log.With("session", id),
		client:  client,
		auth:    events.AuthSnapshot{State: "connecting"},
		bridges: map[string]*Bridge{},
	}
	s.grace = newGraceKeeper(browserGraceWindow, func(callID string) {
		s.log.Warn("call ended: browser did not return within the grace window", "call_id", callID)
		s.terminateCall(callID, core.EndCallReasonUserEnded)
	})
	s.calls = call.NewClient(wa.NewSocket(client), s.log, s.makeExtensions, mgr.maxCalls, s.wireCall, mgr.newObserver)
	client.AddEventHandler(s.handleEvent)
	return s
}

func (s *Session) makeExtensions() []engine.Extension {
	var exts []engine.Extension
	if codec, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions); err == nil {
		wrapped, mode := nativemlow.WrapEncoder(codec)
		if nativemlow.Available() && mode != "native" {
			s.log.Warn("native mlow encoder unavailable; call uses the pure-Go encoder")
		}
		s.log.Debug("audio codec ready", "encoder", mode)
		exts = append(exts, audio.New(opus.WithFallback(wrapped)))
	} else {
		s.log.Warn("MLow codec unavailable; call runs without audio", "err", err)
	}
	return exts
}

func (s *Session) wireCall(callID string, cm *call.CallManager) {
	cm.OnIncoming = func(c *call.CallInfo) {
		raw, _ := types.ParseJID(c.PeerJid)
		pj := resolvePeerJID(context.Background(), s.client, raw)
		peer := pj.String()
		peerName := resolvePeerName(context.Background(), s.client, pj)
		photoURL := cachedPhotoURL(context.Background(), s.mgr.photos, s.id, peer)
		s.mgr.broker.UpsertCall(events.CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: "inbound", Peer: peer,
			PeerName: peerName, PeerPhotoURL: photoURL,
			StartedAt: time.Now().UnixMilli(), Status: events.StatusRinging,
		})
		s.mgr.broker.EmitIncoming(s.id, c.CallID, peer, peerName, photoURL)
		s.mgr.tracer.StartCall(c.CallID, telemetry.CallAttrs{Session: s.id, Peer: c.PeerJid, Direction: "inbound"})
		go s.fetchPeerPhoto(pj, c.CallID)
	}
	cm.OnStateChange = func(c *call.CallInfo) {
		if c.IsEnded() {
			s.mgr.tracer.EndCall(c.CallID, endResult(c), string(c.StateData.EndReason), endDuration(c))
			s.removeCall(c.CallID)
			s.mgr.broker.EndCall(c.CallID, string(c.StateData.EndReason))
			return
		}
		dir := "outbound"
		if c.Direction == core.CallDirectionIncoming {
			dir = "inbound"
		}
		existing, _ := s.mgr.broker.GetCall(c.CallID)
		if existing == nil {
			s.mgr.tracer.StartCall(c.CallID, telemetry.CallAttrs{Session: s.id, Peer: c.PeerJid, Direction: dir})
		}
		if mapStatus(c.StateData.State) == events.StatusConnected && c.StateData.ConnectedAt != nil {
			s.mgr.tracer.MarkActive(c.CallID, c.StateData.ConnectedAt.Sub(c.CreatedAt))
		}
		rec := events.CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: dir, Peer: c.PeerJid,
			StartedAt: time.Now().UnixMilli(), Status: mapStatus(c.StateData.State),
		}
		if existing != nil {
			rec.Owner = existing.Owner
			rec.StartedAt = existing.StartedAt
			rec.Peer = existing.Peer
			rec.PeerName = existing.PeerName
			rec.PeerPhotoURL = existing.PeerPhotoURL
		}
		s.mgr.broker.UpsertCall(rec)
	}
	cm.OnEnded = func(c *call.CallInfo) {
		s.mgr.tracer.EndCall(c.CallID, endResult(c), string(c.StateData.EndReason), endDuration(c))
		s.removeCall(c.CallID)
		s.mgr.broker.EndCall(c.CallID, string(c.StateData.EndReason))
	}
	cm.OnPeerAudio = func(pcm16 []float32) {
		if b := s.getBridge(callID); b != nil {
			_ = b.WritePCM(pcm16)
		}
	}
	cm.OnQuality = func(callID string, q core.CallQuality) {
		s.mgr.broker.EmitCallQuality(s.id, callID, q)
	}
	cm.OnMark = func(callID string, mark string, elapsedMs int64) {
		s.mgr.broker.EmitCallMark(s.id, callID, mark, elapsedMs)
	}
	cm.OnRelay = func(callID, relayName string, rttMs int, hasRtt bool) {
		s.mgr.broker.EmitCallRelay(s.id, callID, relayName, rttMs, hasRtt)
	}
	cm.OnPeerMute = func(callID string, muted bool) {
		s.mgr.broker.EmitCallPeerMute(s.id, callID, muted)
	}
}

func (s *Session) handleEvent(rawEvt any) {
	ctx := context.Background()
	switch evt := rawEvt.(type) {
	case *waevents.Connected:
		if id := s.client.Store.ID; id != nil {
			_ = s.mgr.store.SetJID(s.mgr.appCtx, s.id, id.String())
		}
		go s.fetchOwnPhoto()
		s.setAuth(events.AuthSnapshot{State: "open", Paired: true})
	case *waevents.LoggedOut:
		s.setAuth(events.AuthSnapshot{State: "logged_out", Paired: false})
	case *waevents.CallOffer:
		s.calls.HandleOffer(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *waevents.CallAccept:
		s.calls.HandleAccept(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *waevents.CallTransport:
		s.calls.HandleTransport(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *waevents.CallRelayLatency:
		s.calls.HandleRelayLatency(ctx, wrapCall(evt.From, evt.Data), evt.From)
	case *waevents.CallTerminate:
		s.calls.HandleTerminate(wrapCall(evt.From, evt.Data))
	case *waevents.CallReject:
		s.calls.HandleTerminate(wrapCall(evt.From, evt.Data))
	case *waevents.UnknownCallEvent:
		if _, ok := evt.Node.GetOptionalChildByTag("mute_v2"); ok {
			s.calls.HandleMute(evt.Node)
		}
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
				s.setAuth(events.AuthSnapshot{State: "qr", QR: evt.Code})
				s.mgr.broker.EmitSessionQR(s.id, evt.Code)
			case "success":
				if id := s.client.Store.ID; id != nil {
					_ = s.mgr.store.SetJID(s.mgr.appCtx, s.id, id.String())
				}
				s.setAuth(events.AuthSnapshot{State: "open", Paired: true})
			case "timeout":
				s.setAuth(events.AuthSnapshot{State: "logged_out", Paired: false})
			}
		}
	}()
	return nil
}

func (s *Session) setAuth(a events.AuthSnapshot) {
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
	s.mgr.broker.EmitAuthState(s.id, a)
	s.mgr.broker.EmitSessionList(s.mgr.Infos())
}

func (s *Session) rename(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

func (s *Session) info() events.SessionInfo {
	s.mu.Lock()
	a := s.auth
	name := s.name
	s.mu.Unlock()
	jid, photo := "", ""
	if id := s.client.Store.ID; id != nil {
		jid = id.String()
		photo = cachedPhotoURL(context.Background(), s.mgr.photos, s.id, id.ToNonAD().String())
	}
	return events.SessionInfo{ID: s.id, Name: name, JID: jid, State: a.State, Paired: a.Paired || jid != "", PhotoURL: photo}
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
	// A fresh browser leg re-attached: cancel any pending grace countdown so the call
	// is no longer waiting for the old (refreshed-away) leg to return.
	s.grace.cancel(callID)
	s.bridgeMu.Unlock()
	if old != nil {
		old.Close()
	}
}

// onBridgeDetached runs when a browser leg's peer connection fails or closes. If it
// is still the current leg (not superseded by a re-attach and not already removed),
// it starts the grace countdown instead of ending the call immediately, so a page
// refresh can re-attach. Closing the superseded leg during setBridge/removeCall lands
// here too, but the identity check makes those a no-op.
func (s *Session) onBridgeDetached(callID string, bridge *Bridge) {
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	if s.bridges[callID] != bridge {
		return
	}
	s.log.Info("browser leg detached; holding the call for the grace window", "call_id", callID)
	s.grace.arm(callID)
}

func (s *Session) removeCall(callID string) {
	s.bridgeMu.Lock()
	b := s.bridges[callID]
	delete(s.bridges, callID)
	s.grace.cancel(callID)
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
	s.grace.stopAll()
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

func mapStatus(state core.CallState) events.CallStatus {
	switch state {
	case core.CallStateActive:
		return events.StatusConnected
	case core.CallStateReconnecting:
		return events.StatusReconnecting
	case core.CallStateEnded:
		return events.StatusEnded
	case core.CallStateInitiating:
		return events.StatusStarting
	default:
		return events.StatusRinging
	}
}
