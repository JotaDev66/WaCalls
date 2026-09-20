package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	meowcaller "github.com/purpshell/meowcaller"
	"github.com/rs/zerolog"
)

// meowLogger manda os próprios diagnósticos de chamada/mídia do meowcaller
// pro stderr (onde o wacalls-run.log já captura tudo mais) — sem isso o
// meowcaller usa um logger no-op por padrão, deixando todo o pipeline de
// chamada/mídia invisível.
func meowLogger() zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05.000"}).
		Level(zerolog.DebugLevel).With().Timestamp().Logger()
}

type Session struct {
	id   string
	name string
	mgr  *SessionManager
	log  *slog.Logger

	client *whatsmeow.Client
	meow   *meowcaller.Client
	reg    *callRegistry

	mu   sync.Mutex
	auth AuthSnapshot
}

func newSession(mgr *SessionManager, id, name string, client *whatsmeow.Client) *Session {
	s := &Session{
		id:     id,
		name:   name,
		mgr:    mgr,
		log:    mgr.log.With("session", id),
		client: client,
		meow:   meowcaller.NewClient(client, meowcaller.WithLogger(meowLogger())),
		auth:   AuthSnapshot{State: "connecting"},
		reg:    newCallRegistry(),
	}
	client.AddEventHandler(s.handleEvent)
	s.meow.OnIncomingCall(s.handleIncomingCall)
	return s
}

// mediaKind devolve "video" pra uma chamada de vídeo e "audio" caso
// contrário, pros registros do broker e eventos SSE.
func mediaKind(isVideo bool) string {
	if isVideo {
		return "video"
	}
	return "audio"
}

func mapCallPhase(p meowcaller.CallPhase) CallStatus {
	switch p {
	case meowcaller.CallPhaseActive:
		return StatusConnected
	case meowcaller.CallPhaseEnded:
		return StatusEnded
	case meowcaller.CallPhaseCalling:
		return StatusStarting
	default:
		return StatusRinging
	}
}

// wireCall conecta os callbacks de ciclo de vida/mídia que toda chamada
// (recebida ou feita) precisa, e registra ela no registro de chamadas da
// sessão. direction é "inbound" ou "outbound" — a Call do meowcaller não
// rastreia isso sozinha, quem chama (startOutgoing / handleIncomingCall) já
// sabe pelo contexto.
// extraOnEnd, se não-nil, roda além da contabilidade padrão do broker quando
// a chamada acaba — doTransfer usa isso pra observar uma perna sainte
// específica sem um segundo mecanismo de ciclo de vida (OnEnd/OnStateChange
// do meowcaller só guardam um callback cada, então isso é passado adiante em
// vez de registrado de novo, o que substituiria silenciosamente o do wireCall).
func (s *Session) wireCall(c *meowcaller.Call, direction string, extraOnEnd func(reason string)) {
	callID := c.ID()
	src := newLiveAudioSource()
	// videoSink é plugado no meowcaller AGORA, mesmo sem bridge do browser
	// ainda — senão uma chamada que já nasce em vídeo perde a orientação do
	// primeiro pacote antes do doWebRTC terminar a negociação WebRTC (ver
	// orientedVideoSink em bridge.go).
	videoSink := newOrientedVideoSink()
	s.reg.add(callID, &activeCall{call: c, src: src, videoSink: videoSink})
	c.ReceiveVideo(videoSink)

	rec := func(status CallStatus) CallRecord {
		existing, _ := s.mgr.broker.getCall(callID)
		r := CallRecord{
			SessionID: s.id, CallID: callID, Direction: direction, Peer: c.Peer().String(),
			Media: mediaKind(c.IsVideo()), StartedAt: time.Now().UnixMilli(), Status: status,
		}
		if existing != nil {
			r.Owner = existing.Owner
			r.StartedAt = existing.StartedAt
		}
		return r
	}

	c.OnStateChange(func(p meowcaller.CallPhase) {
		if p == meowcaller.CallPhaseEnded {
			s.removeCall(callID)
			s.mgr.broker.endCall(callID, "ended")
			if extraOnEnd != nil {
				extraOnEnd("ended")
			}
			return
		}
		s.mgr.broker.upsertCall(rec(mapCallPhase(p)))
	})
	c.OnEnd(func(reason string) {
		s.removeCall(callID)
		s.mgr.broker.endCall(callID, reason)
		if extraOnEnd != nil {
			extraOnEnd(reason)
		}
	})
	c.OnVideoKeyframeRequest(func() {
		if b, ok := s.reg.bridge(callID); ok {
			_ = b.RequestKeyframe()
		}
	})
	c.OnReaction(func(r meowcaller.CallReaction) {
		s.mgr.broker.emitReaction(s.id, callID, r.Sender.String(), r.Emoji)
	})
	c.OnHandRaise(func(h meowcaller.HandRaiseState) {
		s.mgr.broker.emitHandRaise(s.id, callID, h.Participant.String(), h.Raised)
	})
	c.OnVideoState(func(v meowcaller.VideoState) {
		// Só emite o evento (o frontend decide se mostra uma notificação de
		// aceitar/recusar) — não aceita sozinho. Ver doVideoAccept em
		// httpapi.go pro aceite explícito.
		s.mgr.broker.emitPeerVideoState(s.id, callID, v.Active, v.Upgrade, v.Orientation)
	})
}

// handleIncomingCall é o único ponto de entrada de chamada recebida do
// meowcaller — substitui o tratamento manual antigo de XML <call>
// offer/accept/transport/terminate, que o meowcaller agora faz internamente.
func (s *Session) handleIncomingCall(c *meowcaller.Call) {
	if max := s.mgr.maxCalls; max > 0 && s.reg.count() >= max {
		s.log.Info("inbound call rejected: session at capacity", "call_id", c.ID())
		_ = c.Reject()
		return
	}
	s.wireCall(c, "inbound", nil)
	s.mgr.broker.upsertCall(CallRecord{
		SessionID: s.id, CallID: c.ID(), Direction: "inbound", Peer: c.Peer().String(),
		Media: mediaKind(c.IsVideo()), StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
	})
	s.mgr.broker.emitIncoming(s.id, c.ID(), c.Peer().String(), mediaKind(c.IsVideo()))
}

// extraOnEnd é opcional (nil no caso normal) — ver a doc de wireCall.
func (s *Session) startOutgoing(ctx context.Context, peer types.JID, isVideo bool, extraOnEnd func(reason string)) (string, error) {
	c, err := s.meow.CallWithOptions(ctx, peer.String(), meowcaller.CallOptions{Video: isVideo})
	if err != nil {
		return "", err
	}
	s.wireCall(c, "outbound", extraOnEnd)
	return c.ID(), nil
}

// startOutgoingGroup faz uma chamada vinculada a um grupo do WhatsApp
// (groupJID tipo "1234567890-1234567890@g.us"), tocando pra todo membro
// atual do grupo.
func (s *Session) startOutgoingGroup(ctx context.Context, groupJID string, isVideo bool) (string, error) {
	c, err := s.meow.GroupCallByIDWithOptions(ctx, groupJID, meowcaller.GroupCallOptions{
		GroupJID: groupJID, Video: isVideo,
	})
	if err != nil {
		return "", err
	}
	s.wireCall(c, "outbound", nil)
	return c.ID(), nil
}

func (s *Session) handleEvent(rawEvt any) {
	switch rawEvt.(type) {
	case *events.Connected:
		if id := s.client.Store.ID; id != nil {
			_ = s.mgr.store.setJID(s.mgr.appCtx, s.id, id.String())
		}
		s.setAuth(AuthSnapshot{State: "open", Paired: true})
	case *events.LoggedOut:
		s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
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

func (s *Session) setBridge(callID string, b *Bridge) {
	oldB, found := s.reg.setBridge(callID, b)
	if !found {
		b.Close()
		return
	}
	if oldB != nil {
		// oldB está sendo substituído por um bridge novo pra mesma chamada
		// (reconexão — reload do browser, instabilidade de rede, ou pickup
		// em outro lugar), não abandonado. CloseQuiet evita cascatear num
		// hangup de verdade via OnTerminalICE do oldB, que doWebRTC conecta
		// a terminateCall.
		oldB.CloseQuiet()
	}
}

func (s *Session) removeCall(callID string) {
	ac, ok := s.reg.remove(callID)
	if !ok {
		return
	}
	if ac.src != nil {
		_ = ac.src.Close()
	}
	if ac.bridge != nil {
		ac.bridge.Close()
	}
}

func (s *Session) terminateCall(callID string, reason string) {
	ac, ok := s.reg.get(callID)
	if !ok {
		return
	}
	_ = reason
	_ = ac.call.Hangup()
}

func (s *Session) teardownAllCalls() {
	for _, ac := range s.reg.drain() {
		_ = ac.call.Hangup()
		if ac.src != nil {
			_ = ac.src.Close()
		}
		if ac.bridge != nil {
			ac.bridge.Close()
		}
	}
}

func (s *Session) replaceClient(client *whatsmeow.Client) {
	s.teardownAllCalls()
	s.client.Disconnect()
	s.client = client
	s.meow = meowcaller.NewClient(client, meowcaller.WithLogger(meowLogger()))
	client.AddEventHandler(s.handleEvent)
	s.meow.OnIncomingCall(s.handleIncomingCall)
}

func (s *Session) shutdown() {
	s.teardownAllCalls()
	s.client.Disconnect()
}
