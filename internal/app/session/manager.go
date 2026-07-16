package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"

	"wacalls/internal/app/events"
	"wacalls/internal/telemetry"
	"wacalls/internal/voip/core"

	"github.com/pion/webrtc/v4"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Manager struct {
	appCtx      context.Context
	container   *sqlstore.Container
	webrtcAPI   *webrtc.API
	broker      *events.Broker
	store       core.SessionStore
	waLogger    waLog.Logger
	log         *slog.Logger
	maxCalls    int
	newObserver func(string) core.CallObserver
	tracer      telemetry.CallTracer
	photos      core.ContactPhotoStore

	mu       sync.RWMutex
	sessions map[string]*Session
	order    []string
}

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type Deps struct {
	Ctx         context.Context
	Container   *sqlstore.Container
	WebRTCAPI   *webrtc.API
	Broker      *events.Broker
	Store       core.SessionStore
	WALogger    waLog.Logger
	Log         *slog.Logger
	MaxCalls    int
	NewObserver func(string) core.CallObserver
	Tracer      telemetry.CallTracer
	Photos      core.ContactPhotoStore
}

func NewManager(d Deps) *Manager {
	if d.NewObserver == nil {
		d.NewObserver = func(string) core.CallObserver { return core.NopObserver{} }
	}
	if d.Tracer == nil {
		d.Tracer = telemetry.NopTracer()
	}
	return &Manager{
		appCtx:      d.Ctx,
		container:   d.Container,
		webrtcAPI:   d.WebRTCAPI,
		broker:      d.Broker,
		store:       d.Store,
		waLogger:    d.WALogger,
		log:         d.Log,
		maxCalls:    d.MaxCalls,
		newObserver: d.NewObserver,
		tracer:      d.Tracer,
		photos:      d.Photos,
		sessions:    map[string]*Session{},
	}
}

func (m *Manager) register(s *Session) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.order = append(m.order, s.id)
	m.mu.Unlock()
}

func (m *Manager) NewSession(id, name string, client *whatsmeow.Client) *Session {
	s := newSession(m, id, name, client)
	m.register(s)
	return s
}

func (m *Manager) unregister(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) Infos() []events.SessionInfo {
	m.mu.RLock()
	ordered := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			ordered = append(ordered, s)
		}
	}
	m.mu.RUnlock()
	out := make([]events.SessionInfo, 0, len(ordered))
	for _, s := range ordered {
		out = append(out, s.info())
	}
	return out
}

func (m *Manager) SnapshotEvents() []any {
	return []any{map[string]any{"type": "session-list", "sessions": m.Infos()}}
}

func (m *Manager) Restore(ctx context.Context) error {
	rows, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.JID == "" {
			_ = m.store.Delete(ctx, row.ID)
			continue
		}
		jid, err := types.ParseJID(row.JID)
		if err != nil {
			m.log.Warn("dropping session with unparseable jid", "session", row.ID, "jid", row.JID)
			_ = m.store.Delete(ctx, row.ID)
			continue
		}
		device, err := m.container.GetDevice(ctx, jid)
		if err != nil || device == nil {
			m.log.Warn("dropping session with no stored device", "session", row.ID, "jid", row.JID, "err", err)
			_ = m.store.Delete(ctx, row.ID)
			continue
		}
		client := whatsmeow.NewClient(device, m.waLogger)
		s := m.NewSession(row.ID, row.Name, client)
		if err := s.connect(ctx); err != nil {
			m.log.Error("session connect failed", "session", row.ID, "err", err)
		}
	}
	m.broker.EmitSessionList(m.Infos())
	m.log.Info("sessions restored", "count", len(m.Infos()))
	return nil
}

func (m *Manager) Create(name string) (string, error) {
	id := newSessionID()
	if err := m.store.Insert(m.appCtx, id, name); err != nil {
		return "", err
	}
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, m.waLogger)
	s := m.NewSession(id, name, client)
	m.broker.EmitSessionList(m.Infos())
	if err := s.startPairing(m.appCtx); err != nil {
		m.log.Error("start pairing failed", "session", id, "err", err)
		return "", fmt.Errorf("start pairing: %w", err)
	}
	m.log.Info("session created", "session", id, "name", name)
	return id, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		if err := s.client.Logout(ctx); err != nil {
			m.log.Warn("logout failed; deleting locally", "session", id, "err", err)
			_ = m.container.DeleteDevice(ctx, s.client.Store)
		}
	} else {
		s.client.Disconnect()
		_ = m.container.DeleteDevice(ctx, s.client.Store)
	}
	s.teardownAllCalls()
	m.unregister(id)
	_ = m.store.Delete(ctx, id)
	m.broker.EmitSessionList(m.Infos())
	m.log.Info("session deleted", "session", id)
	return nil
}

func (m *Manager) Logout(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		if err := s.client.Logout(ctx); err != nil {
			m.log.Warn("logout failed", "session", id, "err", err)
		}
	}
	s.replaceClient(whatsmeow.NewClient(m.container.NewDevice(), m.waLogger))
	_ = m.store.SetJID(ctx, id, "")
	s.setAuth(events.AuthSnapshot{State: "logged_out", Paired: false})
	m.log.Info("session disconnected", "session", id)
	return nil
}

func (m *Manager) Pair(id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		return fmt.Errorf("session already paired")
	}
	s.replaceClient(whatsmeow.NewClient(m.container.NewDevice(), m.waLogger))
	if err := s.startPairing(m.appCtx); err != nil {
		return fmt.Errorf("start pairing: %w", err)
	}
	m.broker.EmitSessionList(m.Infos())
	m.log.Info("session re-pairing", "session", id)
	return nil
}

func (m *Manager) DisconnectAll() {
	m.mu.RLock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.RUnlock()
	for _, s := range all {
		s.shutdown()
	}
}
