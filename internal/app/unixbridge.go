package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wacalls/internal/voip/call"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"

	"go.mau.fi/whatsmeow/types"
)

const (
	unixFrameControlRequest byte = 0x01
	unixFrameControlEvent   byte = 0x02
	unixFrameInboundPCM     byte = 0x10
	unixFrameOutboundPCM    byte = 0x11
	unixFrameFallbackPCM    byte = 0x12

	unixMaxFrameSize      = 1 << 20
	unixPCMRate           = 16_000
	unixPCMChannels       = 1
	unixPCMBytesPerSecond = unixPCMRate * 2
	unixMaxFallbackBytes  = 5 * unixPCMBytesPerSecond
	unixMaxPlaybackBytes  = 10 * unixPCMBytesPerSecond
	unixPCMChunkBytes     = 640 // 20 ms of mono PCM16 at 16 kHz.
	unixCodecFrameBytes   = 1_920
)

var errUnixSessionDone = errors.New("local PCM session finished")

type localCallEvent struct {
	Event  string `json:"event"`
	CallID string `json:"call_id,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (e localCallEvent) terminal() bool {
	return e.Event == "busy" || e.Event == "ended" || e.Event == "error"
}

type localPCMCall interface {
	InboundPCM() <-chan []byte
	Events() <-chan localCallEvent
	SendPCM([]byte) error
	PlayFallback([]byte) error
	FlushPlayback() error
	End(context.Context, string) error
}

type localPCMBackend interface {
	StartOutbound(context.Context, string, string) (localPCMCall, error)
}

// unixPCMServer owns a private AF_UNIX listener. It deliberately has no HTTP
// handler and does not expose WhatsApp session or QR material.
type unixPCMServer struct {
	path    string
	backend localPCMBackend
	log     *slog.Logger

	mu       sync.Mutex
	listener net.Listener
	active   *unixPCMConn
	closed   bool
	wg       sync.WaitGroup
}

func newUnixPCMServer(path string, backend localPCMBackend, log *slog.Logger) *unixPCMServer {
	if log == nil {
		log = slog.Default()
	}
	return &unixPCMServer{path: path, backend: backend, log: log}
}

func (s *unixPCMServer) Start(ctx context.Context) error {
	if !filepath.IsAbs(s.path) {
		return fmt.Errorf("local PCM socket path must be absolute")
	}
	dir := filepath.Dir(s.path)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create local PCM socket directory: %w", err)
		}
		if err := os.Chmod(dir, 0o750); err != nil {
			return fmt.Errorf("secure local PCM socket directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect local PCM socket directory: %w", err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("local PCM socket parent must be a real directory")
	} else if info.Mode().Perm() != 0o750 {
		return fmt.Errorf("local PCM socket directory permissions are %#o, want 0750", info.Mode().Perm())
	}
	if err := removeStaleUnixSocket(s.path); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("listen on local PCM socket: %w", err)
	}
	if err := os.Chmod(s.path, 0o660); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.path)
		return fmt.Errorf("secure local PCM socket: %w", err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	s.wg.Add(1)
	go s.acceptLoop(ctx, listener)
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	s.log.Info("local PCM socket listening", "path", s.path)
	return nil
}

func (s *unixPCMServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	listener := s.listener
	active := s.active
	s.mu.Unlock()

	if active != nil {
		active.close()
	}
	var err error
	if listener != nil {
		err = listener.Close()
	}
	s.wg.Wait()
	if removeErr := os.Remove(s.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
		err = removeErr
	}
	return err
}

func (s *unixPCMServer) acceptLoop(ctx context.Context, listener net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				s.log.Error("local PCM accept failed", "err", err)
			}
			return
		}
		client := &unixPCMConn{server: s, conn: conn, log: s.log}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			client.serve(ctx)
		}()
	}
}

func (s *unixPCMServer) claim(client *unixPCMConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil {
		return false
	}
	s.active = client
	return true
}

func (s *unixPCMServer) release(client *unixPCMConn) {
	s.mu.Lock()
	if s.active == client {
		s.active = nil
	}
	s.mu.Unlock()
}

func removeStaleUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect local PCM socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket path %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale local PCM socket: %w", err)
	}
	return nil
}

type unixPCMConn struct {
	server *unixPCMServer
	conn   net.Conn
	log    *slog.Logger

	writeMu sync.Mutex
	closeMu sync.Once
	call    localPCMCall
	callID  string
	claimed bool
	ended   bool
}

func (c *unixPCMConn) serve(serverCtx context.Context) {
	defer func() {
		if c.call != nil && !c.ended {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.call.End(ctx, "worker_disconnected")
			cancel()
		}
		if c.claimed {
			c.server.release(c)
		}
		c.close()
	}()

	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		frameType, payload, err := readUnixFrame(c.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && serverCtx.Err() == nil {
				c.log.Debug("local PCM connection closed", "err", err)
			}
			return
		}
		if err := c.handleFrame(serverCtx, frameType, payload); err != nil {
			if !errors.Is(err, errUnixSessionDone) {
				_ = c.writeEvent(localCallEvent{Event: "error", CallID: c.callID, Error: err.Error()})
			}
			return
		}
	}
}

func (c *unixPCMConn) handleFrame(ctx context.Context, frameType byte, payload []byte) error {
	switch frameType {
	case unixFrameControlRequest:
		return c.handleControl(ctx, payload)
	case unixFrameOutboundPCM:
		if c.call == nil {
			return errors.New("start command required before PCM")
		}
		return c.call.SendPCM(payload)
	case unixFrameFallbackPCM:
		if c.call == nil {
			return errors.New("start command required before fallback PCM")
		}
		return c.call.PlayFallback(payload)
	default:
		return fmt.Errorf("frame type 0x%02x is not accepted from worker", frameType)
	}
}

func (c *unixPCMConn) handleControl(ctx context.Context, payload []byte) error {
	var envelope struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode control command: %w", err)
	}
	switch envelope.Op {
	case "start":
		if c.call != nil || c.claimed {
			return errors.New("call already started on this connection")
		}
		var command struct {
			Op         string `json:"op"`
			CallID     string `json:"call_id"`
			To         string `json:"to"`
			SampleRate int    `json:"sample_rate"`
			Channels   int    `json:"channels"`
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&command); err != nil {
			return fmt.Errorf("decode start command: %w", err)
		}
		if err := validateStartCommand(command.CallID, command.To, command.SampleRate, command.Channels); err != nil {
			return err
		}
		c.callID = command.CallID
		if !c.server.claim(c) {
			_ = c.writeEvent(localCallEvent{Event: "busy", CallID: command.CallID, Reason: "capacity"})
			return errUnixSessionDone
		}
		c.claimed = true
		localCall, err := c.server.backend.StartOutbound(ctx, command.CallID, command.To)
		if err != nil {
			c.server.release(c)
			c.claimed = false
			return fmt.Errorf("start outbound call: %w", err)
		}
		c.call = localCall
		_ = c.conn.SetReadDeadline(time.Time{})
		go c.forward(localCall)
		return nil
	case "flush_playback":
		if c.call == nil {
			return errors.New("start command required before flush")
		}
		return c.call.FlushPlayback()
	case "end":
		if c.call == nil {
			return errors.New("start command required before end")
		}
		var command struct {
			Op     string `json:"op"`
			Reason string `json:"reason"`
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&command); err != nil {
			return fmt.Errorf("decode end command: %w", err)
		}
		command.Reason = strings.TrimSpace(command.Reason)
		if command.Reason == "" {
			command.Reason = "worker_ended"
		}
		if len(command.Reason) > 128 {
			return errors.New("end reason exceeds 128 bytes")
		}
		c.ended = true
		endCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		err := c.call.End(endCtx, command.Reason)
		cancel()
		if err != nil {
			return fmt.Errorf("end outbound call: %w", err)
		}
		return errUnixSessionDone
	default:
		return fmt.Errorf("unsupported control operation %q", envelope.Op)
	}
}

func (c *unixPCMConn) forward(localCall localPCMCall) {
	pcm := localCall.InboundPCM()
	events := localCall.Events()
	for pcm != nil || events != nil {
		select {
		case payload, ok := <-pcm:
			if !ok {
				pcm = nil
				continue
			}
			if err := validatePCM(payload, unixMaxFrameSize); err != nil {
				_ = c.writeEvent(localCallEvent{Event: "error", CallID: c.callID, Error: err.Error()})
				c.close()
				return
			}
			if err := c.writeFrame(unixFrameInboundPCM, payload); err != nil {
				return
			}
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if event.CallID == "" {
				event.CallID = c.callID
			}
			if err := c.writeEvent(event); err != nil {
				return
			}
			if event.terminal() {
				c.close()
				return
			}
		}
	}
}

func (c *unixPCMConn) writeEvent(event localCallEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return c.writeFrame(unixFrameControlEvent, payload)
}

func (c *unixPCMConn) writeFrame(frameType byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return writeUnixFrame(c.conn, frameType, payload)
}

func (c *unixPCMConn) close() {
	c.closeMu.Do(func() { _ = c.conn.Close() })
}

func validateStartCommand(callID, destination string, sampleRate, channels int) error {
	if !validLocalCallID(callID) {
		return errors.New("call_id must contain 1 to 128 safe ASCII characters")
	}
	if sampleRate != unixPCMRate || channels != unixPCMChannels {
		return fmt.Errorf("PCM format must be mono 16 kHz")
	}
	if !validE164(destination) {
		return errors.New("destination must be E.164")
	}
	return nil
}

func validLocalCallID(callID string) bool {
	if len(callID) == 0 || len(callID) > 128 {
		return false
	}
	for _, r := range callID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func validE164(destination string) bool {
	if len(destination) < 9 || len(destination) > 16 || destination[0] != '+' {
		return false
	}
	for _, r := range destination[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return destination[1] != '0'
}

func validatePCM(payload []byte, maximum int) error {
	if len(payload) == 0 || len(payload)%2 != 0 {
		return errors.New("PCM16 payload must be non-empty and even-sized")
	}
	if len(payload) > maximum {
		return fmt.Errorf("PCM16 payload exceeds %d bytes", maximum)
	}
	return nil
}

func readUnixFrame(reader io.Reader) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > unixMaxFrameSize {
		return 0, nil, fmt.Errorf("local PCM frame size %d exceeds maximum", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	return header[0], payload, nil
}

func writeUnixFrame(writer io.Writer, frameType byte, payload []byte) error {
	if len(payload) > unixMaxFrameSize {
		return fmt.Errorf("local PCM frame exceeds %d bytes", unixMaxFrameSize)
	}
	header := make([]byte, 5)
	header[0] = frameType
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if err := writeAll(writer, header); err != nil {
		return err
	}
	return writeAll(writer, payload)
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}

type sessionLocalBackend struct {
	sessions *SessionManager
}

func (b sessionLocalBackend) StartOutbound(ctx context.Context, externalCallID, destination string) (localPCMCall, error) {
	session, err := b.sessions.singleReadySession()
	if err != nil {
		return nil, err
	}
	if b.sessions.activeCallCount() != 0 {
		return nil, errors.New("another WhatsApp call is active")
	}
	peer := types.NewJID(normalizePhone(destination), types.DefaultUserServer)
	managed := newManagedLocalCall(externalCallID)
	managed.emit(localCallEvent{Event: "dialing"})
	if _, err := session.startLocalOutgoing(ctx, peer, managed); err != nil {
		managed.closed()
		return nil, err
	}
	if !managed.isBound() {
		managed.closed()
		return nil, errors.New("local call media was not bound")
	}
	return managed, nil
}

func (m *SessionManager) singleReadySession() (*Session, error) {
	m.mu.RLock()
	ordered := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if session := m.sessions[id]; session != nil {
			ordered = append(ordered, session)
		}
	}
	m.mu.RUnlock()

	ready := make([]*Session, 0, 1)
	for _, session := range ordered {
		info := session.info()
		if info.Paired && info.State == "open" {
			ready = append(ready, session)
		}
	}
	if len(ready) == 0 {
		return nil, errors.New("no paired WhatsApp session is ready")
	}
	if len(ready) != 1 {
		return nil, errors.New("exactly one paired WhatsApp session is required")
	}
	return ready[0], nil
}

func (m *SessionManager) activeCallCount() int {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.RUnlock()
	total := 0
	for _, session := range sessions {
		total += session.callCount()
	}
	return total
}

type localCallObserver interface {
	bound(*Session, string, localCallMedia)
	onPeerPCM([]float32)
	onState(*call.CallInfo)
	closed()
}

type localCallMedia interface {
	FeedCapturedPCM([]float32)
	FlushCapturedPCM()
	WaitCapturedPCMDrained(context.Context) error
	EndCall(context.Context, core.EndCallReason) error
}

type managedLocalCall struct {
	externalCallID string
	pcm            chan []byte
	events         chan localCallEvent

	mu           sync.Mutex
	media        localCallMedia
	playback     *pcmPlayback
	fallbackDone <-chan struct{}
	lastEvent    string
	terminal     bool
	endStarted   bool
	backpressure bool
}

func newManagedLocalCall(externalCallID string) *managedLocalCall {
	return &managedLocalCall{
		externalCallID: externalCallID,
		pcm:            make(chan []byte, 32),
		events:         make(chan localCallEvent, 16),
	}
}

func (m *managedLocalCall) bound(_ *Session, _ string, mediaCall localCallMedia) {
	m.mu.Lock()
	m.media = mediaCall
	m.playback = newPCMPlayback(mediaCall.FeedCapturedPCM, mediaCall.FlushCapturedPCM)
	m.mu.Unlock()
}

func (m *managedLocalCall) isBound() bool {
	m.mu.Lock()
	bound := m.media != nil && m.playback != nil
	m.mu.Unlock()
	return bound
}

func (m *managedLocalCall) InboundPCM() <-chan []byte     { return m.pcm }
func (m *managedLocalCall) Events() <-chan localCallEvent { return m.events }

func (m *managedLocalCall) SendPCM(payload []byte) error {
	if err := validatePCM(payload, unixMaxFrameSize); err != nil {
		return err
	}
	m.mu.Lock()
	playback := m.playback
	m.mu.Unlock()
	if playback == nil {
		return errors.New("call media is not ready")
	}
	_, err := playback.Enqueue(payload, false)
	return err
}

func (m *managedLocalCall) PlayFallback(payload []byte) error {
	if err := validatePCM(payload, unixMaxFallbackBytes); err != nil {
		return err
	}
	m.mu.Lock()
	playback := m.playback
	m.mu.Unlock()
	if playback == nil {
		return errors.New("call media is not ready")
	}
	playback.Flush()
	padded := padPCM(payload, unixCodecFrameBytes)
	done, err := playback.Enqueue(padded, true)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.fallbackDone = done
	m.mu.Unlock()
	return nil
}

func (m *managedLocalCall) FlushPlayback() error {
	m.mu.Lock()
	playback := m.playback
	m.fallbackDone = nil
	m.mu.Unlock()
	if playback == nil {
		return errors.New("call media is not ready")
	}
	playback.Flush()
	return nil
}

func (m *managedLocalCall) End(_ context.Context, reason string) error {
	m.mu.Lock()
	if m.endStarted {
		m.mu.Unlock()
		return nil
	}
	m.endStarted = true
	mediaCall := m.media
	playback := m.playback
	fallbackDone := m.fallbackDone
	m.mu.Unlock()
	if mediaCall == nil {
		return errors.New("call media is not ready")
	}

	var drainErr error
	if fallbackDone != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), 5250*time.Millisecond)
		select {
		case <-fallbackDone:
			drainErr = mediaCall.WaitCapturedPCMDrained(drainCtx)
		case <-drainCtx.Done():
			drainErr = drainCtx.Err()
		}
		cancel()
	}
	endCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	endErr := mediaCall.EndCall(endCtx, mapLocalEndReason(reason))
	cancel()
	if playback != nil {
		playback.Close()
	}
	if drainErr != nil {
		return fmt.Errorf("drain fallback PCM: %w", drainErr)
	}
	return endErr
}

func (m *managedLocalCall) onPeerPCM(pcm []float32) {
	payload := media.PCMFloat32ToInt16LE(pcm)
	if len(payload) == 0 {
		return
	}
	select {
	case m.pcm <- payload:
	default:
		m.mu.Lock()
		alreadyFailed := m.backpressure
		m.backpressure = true
		m.mu.Unlock()
		if !alreadyFailed {
			m.emit(localCallEvent{Event: "error", Error: "worker PCM backpressure"})
			go func() { _ = m.End(context.Background(), "worker_pcm_backpressure") }()
		}
	}
}

func (m *managedLocalCall) onState(info *call.CallInfo) {
	if info == nil {
		return
	}
	event := localCallEvent{}
	switch info.StateData.State {
	case core.CallStateInitiating:
		event.Event = "dialing"
	case core.CallStateRinging, core.CallStateIncomingRinging, core.CallStateConnecting:
		event.Event = "ringing"
	case core.CallStateActive, core.CallStateOnHold:
		event.Event = "connected"
	case core.CallStateEnded:
		event.Reason = string(info.StateData.EndReason)
		if info.StateData.EndReason == core.EndCallReasonBusy {
			event.Event = "busy"
		} else {
			event.Event = "ended"
		}
	default:
		return
	}
	m.emit(event)
	if event.terminal() {
		m.closed()
	}
}

func (m *managedLocalCall) emit(event localCallEvent) {
	event.CallID = m.externalCallID
	m.mu.Lock()
	if m.terminal || event.Event == m.lastEvent {
		m.mu.Unlock()
		return
	}
	m.lastEvent = event.Event
	if event.terminal() {
		m.terminal = true
	}
	m.mu.Unlock()
	select {
	case m.events <- event:
	default:
		go func() { _ = m.End(context.Background(), "worker_event_backpressure") }()
	}
}

func (m *managedLocalCall) closed() {
	m.mu.Lock()
	playback := m.playback
	m.mu.Unlock()
	if playback != nil {
		playback.Close()
	}
}

func mapLocalEndReason(reason string) core.EndCallReason {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "cancel"):
		return core.EndCallReasonCancelled
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "duration"):
		return core.EndCallReasonTimeout
	case strings.Contains(lower, "busy"):
		return core.EndCallReasonBusy
	case strings.Contains(lower, "fail"), strings.Contains(lower, "error"), strings.Contains(lower, "unavailable"):
		return core.EndCallReasonFailed
	default:
		return core.EndCallReasonUserEnded
	}
}

func padPCM(payload []byte, multiple int) []byte {
	length := len(payload)
	paddedLength := ((length + multiple - 1) / multiple) * multiple
	padded := make([]byte, paddedLength)
	copy(padded, payload)
	return padded
}

type playbackSegment struct {
	data []byte
	done chan struct{}
}

type pcmPlayback struct {
	feed     func([]float32)
	flush    func()
	interval time.Duration
	chunk    int
	maximum  int

	actionMu sync.Mutex
	mu       sync.Mutex
	segments []playbackSegment
	queued   int
	closed   bool
	stop     chan struct{}
	stopOnce sync.Once
}

func newPCMPlayback(feed func([]float32), flush func()) *pcmPlayback {
	p := &pcmPlayback{
		feed: feed, flush: flush, interval: 20 * time.Millisecond,
		chunk: unixPCMChunkBytes, maximum: unixMaxPlaybackBytes, stop: make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *pcmPlayback) Enqueue(payload []byte, fallback bool) (<-chan struct{}, error) {
	if err := validatePCM(payload, p.maximum); err != nil {
		return nil, err
	}
	copyPayload := append([]byte(nil), payload...)
	var done chan struct{}
	if fallback {
		done = make(chan struct{})
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, net.ErrClosed
	}
	if p.queued+len(copyPayload) > p.maximum {
		p.mu.Unlock()
		return nil, errors.New("PCM playback queue is full")
	}
	p.segments = append(p.segments, playbackSegment{data: copyPayload, done: done})
	p.queued += len(copyPayload)
	p.mu.Unlock()
	if fallback {
		p.pumpOnce()
	}
	return done, nil
}

func (p *pcmPlayback) Flush() {
	p.actionMu.Lock()
	p.mu.Lock()
	p.discardLocked()
	p.mu.Unlock()
	p.flush()
	p.actionMu.Unlock()
}

func (p *pcmPlayback) Close() {
	p.stopOnce.Do(func() {
		p.actionMu.Lock()
		p.mu.Lock()
		p.closed = true
		p.discardLocked()
		p.mu.Unlock()
		p.flush()
		close(p.stop)
		p.actionMu.Unlock()
	})
}

func (p *pcmPlayback) run() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.pumpOnce()
		case <-p.stop:
			return
		}
	}
}

func (p *pcmPlayback) pumpOnce() {
	p.actionMu.Lock()
	defer p.actionMu.Unlock()
	p.mu.Lock()
	if p.closed || len(p.segments) == 0 {
		p.mu.Unlock()
		return
	}
	payload := make([]byte, 0, p.chunk)
	completed := make([]chan struct{}, 0, 1)
	for len(payload) < p.chunk && len(p.segments) > 0 {
		segment := &p.segments[0]
		n := p.chunk - len(payload)
		if len(segment.data) < n {
			n = len(segment.data)
		}
		payload = append(payload, segment.data[:n]...)
		segment.data = segment.data[n:]
		p.queued -= n
		if len(segment.data) == 0 {
			if segment.done != nil {
				completed = append(completed, segment.done)
			}
			p.segments = p.segments[1:]
		}
	}
	p.mu.Unlock()
	p.feed(media.PCMInt16LEToFloat32(payload))
	for _, done := range completed {
		close(done)
	}
}

func (p *pcmPlayback) discardLocked() {
	for _, segment := range p.segments {
		if segment.done != nil {
			close(segment.done)
		}
	}
	p.segments = nil
	p.queued = 0
}
