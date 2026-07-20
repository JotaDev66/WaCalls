package transport

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
	"wacalls/internal/voip/core"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

const (
	relayConnectionTimeout = 20 * time.Second
	relayKeepaliveInterval = 1100 * time.Millisecond

	registrationRefreshTicks = 5

	peerConnectionBytes = 64 * 1024
	dataChannelBytes    = 16 * 1024

	maxDialRelays = 3

	iceDisconnectedTimeout = 5 * time.Second
	iceFailedTimeout       = 25 * time.Second
	iceKeepaliveInterval   = 2 * time.Second
)

var relayAPI = func() *webrtc.API {
	s := webrtc.SettingEngine{}
	s.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	s.SetICETimeouts(iceDisconnectedTimeout, iceFailedTimeout, iceKeepaliveInterval)
	return webrtc.NewAPI(webrtc.WithSettingEngine(s))
}()

type relayConnState int

const (
	relayStateConnecting relayConnState = iota
	relayStateOpen
	relayStateClosed
	relayStateFailed
)

type RelayConfig struct {
	IP           string
	Port         int
	Token        string
	AuthToken    string
	RawAuthToken []byte
	RawToken     []byte
	Key          string
	RelayID      int
	Name         string
	AuthTokenID  string
}

type relayConnection struct {
	state        atomic.Int32
	degraded     atomic.Bool
	pc           *webrtc.PeerConnection
	channel      *webrtc.DataChannel
	id           string
	info         RelayConfig
	localUfrag   string
	keepalive    *time.Ticker
	stopCh       chan struct{}
	mem          int64
	teardownOnce sync.Once
}

func (c *relayConnection) getState() relayConnState  { return relayConnState(c.state.Load()) }
func (c *relayConnection) setState(s relayConnState) { c.state.Store(int32(s)) }

type SctpRelayManager struct {
	mu          sync.Mutex
	connections map[string]*relayConnection
	log         *slog.Logger

	audioSsrc        atomic.Uint32
	subscriptionSsrc atomic.Uint32
	streamSelfSsrcs  []uint32
	streamPeerSsrcs  []uint32

	onConnected func(ip string, port int)

	onReceive func(data []byte)

	lastUsable     int
	onUsableChange func(usable int)

	observer atomic.Pointer[core.CallObserver]
}

func (m *SctpRelayManager) obs() core.CallObserver { return *m.observer.Load() }

func (m *SctpRelayManager) streamSsrcsSnapshot() (self, peer []uint32) {
	m.mu.Lock()
	self = append([]uint32(nil), m.streamSelfSsrcs...)
	peer = append([]uint32(nil), m.streamPeerSsrcs...)
	m.mu.Unlock()
	return
}

func NewSctpRelayManager(log *slog.Logger) *SctpRelayManager {
	if log == nil {
		log = slog.Default()
	}
	m := &SctpRelayManager{
		connections: map[string]*relayConnection{},
		log:         log,
	}
	var nop core.CallObserver = core.NopObserver{}
	m.observer.Store(&nop)
	return m
}

func (m *SctpRelayManager) SetSsrc(ssrc uint32) { m.audioSsrc.Store(ssrc) }

func (m *SctpRelayManager) SetSubscriptionSsrc(ssrc uint32) { m.subscriptionSsrc.Store(ssrc) }

func (m *SctpRelayManager) SetStreamSsrcs(selfSsrcs, peerSsrcs []uint32) {
	m.mu.Lock()
	m.streamSelfSsrcs = append(m.streamSelfSsrcs[:0], selfSsrcs...)
	m.streamPeerSsrcs = append(m.streamPeerSsrcs[:0], peerSsrcs...)
	m.mu.Unlock()
}

func (m *SctpRelayManager) SetOnConnected(fn func(ip string, port int)) { m.onConnected = fn }

func (m *SctpRelayManager) SetOnReceive(fn func(data []byte)) { m.onReceive = fn }

func (m *SctpRelayManager) SetOnUsableChange(fn func(usable int)) { m.onUsableChange = fn }

func (m *SctpRelayManager) SetObserver(o core.CallObserver) {
	if o == nil {
		o = core.NopObserver{}
	}
	m.observer.Store(&o)
}

func (m *SctpRelayManager) ResendSubscriptions() {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		if c.getState() == relayStateOpen && c.channel != nil {
			m.sendStunRegistration(c)
		}
	}
}

func connID(ip string, port int, authTokenID string) string {
	base := fmt.Sprintf("%s:%d", ip, port)
	if authTokenID != "" {
		return base + "#" + authTokenID
	}
	return base
}

func (m *SctpRelayManager) ConfigureRelays(relays []RelayConfig) {
	var wg sync.WaitGroup
	m.mu.Lock()
	dialed := len(m.connections)
	m.mu.Unlock()
	for _, r := range relays {
		if dialed >= maxDialRelays {
			break
		}
		port := r.Port
		if port == 0 {
			port = core.WARelayPort
		}
		r.Port = port
		id := connID(r.IP, port, r.AuthTokenID)
		m.mu.Lock()
		_, exists := m.connections[id]
		m.mu.Unlock()
		if exists {
			continue
		}
		dialed++
		wg.Add(1)
		dialDone := m.obs().TrackGoroutine()
		go func(rc RelayConfig) {
			defer wg.Done()
			defer dialDone()
			m.connectToRelay(rc)
		}(r)
	}
	wg.Wait()
}

func (m *SctpRelayManager) connectToRelay(info RelayConfig) {
	id := connID(info.IP, info.Port, info.AuthTokenID)
	m.log.Info("relay connecting", "id", id, "name", info.Name)

	conn := &relayConnection{
		id:     id,
		info:   info,
		stopCh: make(chan struct{}),
	}
	m.mu.Lock()
	m.connections[id] = conn
	m.mu.Unlock()

	pc, err := relayAPI.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		m.log.Error("relay peerconnection failed", "id", id, "err", err)
		m.failConnection(conn)
		return
	}
	conn.pc = pc
	conn.mem += peerConnectionBytes
	m.obs().AddMem(peerConnectionBytes)

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) { m.handleICEState(conn, s) })
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnected {
			m.obs().Mark(core.MarkTransportDTLS)
		}
	})

	ordered := false
	channel, err := pc.CreateDataChannel("wa-web-call", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		m.log.Error("relay datachannel failed", "id", id, "err", err)
		m.failConnection(conn)
		return
	}
	conn.channel = channel
	conn.mem += dataChannelBytes
	m.obs().AddMem(dataChannelBytes)

	channel.OnOpen(func() {
		m.mu.Lock()
		conn.setState(relayStateOpen)
		m.mu.Unlock()
		m.recomputeHealth()
		m.log.Info("relay datachannel open", "id", id)
		m.obs().Mark(core.MarkTransportSCTPOpen)
		m.sendStunRegistration(conn)
		m.obs().Mark(core.MarkTransportSTUN)
		m.startKeepalive(conn)
		if m.onConnected != nil {
			m.onConnected(info.IP, info.Port)
		}
	})
	channel.OnClose(func() { m.closeConnection(id) })
	channel.OnMessage(func(msg webrtc.DataChannelMessage) {
		if m.onReceive != nil {
			m.onReceive(msg.Data)
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		m.failConnection(conn)
		return
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		m.failConnection(conn)
		return
	}

	conn.localUfrag = extractFirst(reUfrag, offer.SDP)
	munged := m.modifySdpForRelay(offer.SDP, info)

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: munged}); err != nil {
		m.log.Error("relay set remote description failed", "id", id, "err", err)
		m.failConnection(conn)
		return
	}

	watchdogDone := m.obs().TrackGoroutine()
	go func() {
		defer watchdogDone()
		select {
		case <-time.After(relayConnectionTimeout):
			if conn.getState() == relayStateConnecting {
				m.log.Debug("relay connection timeout", "id", id)
				m.failConnection(conn)
			}
		case <-conn.stopCh:
		}
	}()
}

var (
	reSetup       = regexp.MustCompile(`a=setup:actpass`)
	reUfragLine   = regexp.MustCompile(`a=ice-ufrag:[^\r\n]+`)
	rePwdLine     = regexp.MustCompile(`a=ice-pwd:[^\r\n]+`)
	reFingerprint = regexp.MustCompile(`a=fingerprint:[^\r\n]+`)
	reMaxMsg      = regexp.MustCompile(`a=max-message-size:[^\r\n]+`)
	reIceOptions  = regexp.MustCompile(`a=ice-options:[^\r\n]+\r?\n`)
	reCandidate   = regexp.MustCompile(`a=candidate:[^\r\n]+\r?\n`)
	reEndCand     = regexp.MustCompile(`a=end-of-candidates\r?\n?`)
	reUfrag       = regexp.MustCompile(`a=ice-ufrag:([^\r\n]+)`)
)

func (m *SctpRelayManager) modifySdpForRelay(sdp string, info RelayConfig) string {
	out := reSetup.ReplaceAllString(sdp, "a=setup:passive")

	iceUfrag := info.AuthToken
	if iceUfrag == "" {
		iceUfrag = info.Token
	}
	out = reUfragLine.ReplaceAllString(out, "a=ice-ufrag:"+iceUfrag)
	out = rePwdLine.ReplaceAllString(out, "a=ice-pwd:"+info.Key)
	out = reFingerprint.ReplaceAllString(out, "a=fingerprint:"+core.WADTLSFingerprint)
	out = reMaxMsg.ReplaceAllString(out, "a=max-message-size:1500")
	out = reIceOptions.ReplaceAllString(out, "")

	out = reCandidate.ReplaceAllString(out, "")
	out = reEndCand.ReplaceAllString(out, "")
	candidate := fmt.Sprintf("a=candidate:2 1 udp 2122262783 %s %d typ host generation 0 network-cost 5", info.IP, info.Port)
	out += candidate + "\r\n" + "a=end-of-candidates" + "\r\n"
	return out
}

func extractFirst(re *regexp.Regexp, s string) string {
	if mm := re.FindStringSubmatch(s); len(mm) > 1 {
		return mm[1]
	}
	return ""
}

func (m *SctpRelayManager) sendRegistration(conn *relayConnection) {
	info := conn.info
	remoteUfrag := info.AuthToken
	if remoteUfrag == "" {
		remoteUfrag = info.Token
	}
	if remoteUfrag == "" {
		return
	}
	localUfrag := conn.localUfrag
	hmacKey := []byte(info.Key)

	if conn.getState() != relayStateOpen || conn.channel == nil {
		return
	}
	ssrc := m.subscriptionSsrc.Load()
	if ssrc == 0 {
		ssrc = m.audioSsrc.Load()
	}
	if ssrc == 0 {
		return
	}
	subs := BuildSenderSubscriptions(ssrc)

	if localUfrag != "" {
		username := []byte(remoteUfrag + ":" + localUfrag)
		m.sendRaw(conn, BuildBindingRequestWithSubs(username, hmacKey, subs, true, true))
	}
	if info.Token != "" && info.Token != remoteUfrag && localUfrag != "" {
		username := []byte(info.Token + ":" + localUfrag)
		m.sendRaw(conn, BuildBindingRequestWithSubs(username, hmacKey, subs, true, true))
	}
	m.sendRaw(conn, BuildBindingRequestWithSubs(nil, nil, subs, false, false))

	if len(info.RawToken) > 0 {
		selfSsrcs, peerSsrcs := m.streamSsrcsSnapshot()
		if len(selfSsrcs) == 0 {
			selfSsrcs = []uint32{m.audioSsrc.Load()}
			peerSsrcs = nil
			if sub := m.subscriptionSsrc.Load(); sub != 0 {
				peerSsrcs = []uint32{sub}
			}
		}
		ssrcList := BuildSSRCSubscriptionList(selfSsrcs, peerSsrcs, 0, 0)
		m.sendRaw(conn, BuildAllocateForRelay(info.RawToken, ssrcList, hmacKey, info.IP, info.Port))
	}
}

func (m *SctpRelayManager) sendStunRegistration(conn *relayConnection) {
	m.sendRegistration(conn)
	for _, d := range []time.Duration{50, 150, 500, 3000} {
		delay := d * time.Millisecond
		retransmitDone := m.obs().TrackGoroutine()
		go func() {
			defer retransmitDone()
			select {
			case <-time.After(delay):
				m.mu.Lock()
				open := conn.getState() == relayStateOpen
				m.mu.Unlock()
				if open {
					m.sendRegistration(conn)
				}
			case <-conn.stopCh:
			}
		}()
	}
}

func (m *SctpRelayManager) startKeepalive(conn *relayConnection) {
	m.sendRaw(conn, BuildWhatsAppPing())
	ticker := time.NewTicker(relayKeepaliveInterval)
	conn.keepalive = ticker
	keepaliveDone := m.obs().TrackGoroutine()
	go func() {
		defer keepaliveDone()
		ticks := 0
		for {
			select {
			case <-ticker.C:
				if conn.getState() != relayStateOpen || conn.channel == nil {
					return
				}
				m.sendRaw(conn, BuildWhatsAppPing())
				ticks++
				if ticks%registrationRefreshTicks == 0 {
					m.sendRegistration(conn)
				}
			case <-conn.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

func (m *SctpRelayManager) sendRaw(conn *relayConnection, data []byte) {
	if conn.channel == nil || conn.getState() != relayStateOpen {
		return
	}
	if err := conn.channel.Send(data); err != nil {
		m.log.Debug("relay send error", "id", conn.id, "err", err)
	}
}

func (m *SctpRelayManager) Broadcast(data []byte) {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		m.sendRaw(c, data)
	}
}

func (m *SctpRelayManager) BufferedAmount() uint64 {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	var maxBuf uint64
	for _, c := range conns {
		if c.getState() == relayStateOpen && c.channel != nil {
			if b := c.channel.BufferedAmount(); b > maxBuf {
				maxBuf = b
			}
		}
	}
	return maxBuf
}

func (m *SctpRelayManager) handleICEState(conn *relayConnection, s webrtc.ICEConnectionState) {
	m.log.Info("relay ice state", "id", conn.id, "state", s.String())
	switch s {
	case webrtc.ICEConnectionStateConnected:
		m.obs().Mark(core.MarkTransportICE)
		if conn.degraded.Swap(false) {
			m.log.Info("relay ice recovered", "id", conn.id)
			m.sendStunRegistration(conn)
		}
		m.recomputeHealth()
	case webrtc.ICEConnectionStateDisconnected:
		m.log.Warn("relay ice disconnected; waiting for recovery", "id", conn.id)
		conn.degraded.Store(true)
		m.recomputeHealth()
	case webrtc.ICEConnectionStateFailed:
		m.failConnection(conn)
	default:
	}
}

func (m *SctpRelayManager) usableLocked() int {
	n := 0
	for _, c := range m.connections {
		if c.getState() == relayStateOpen && !c.degraded.Load() {
			n++
		}
	}
	return n
}

func (m *SctpRelayManager) recomputeHealth() {
	m.mu.Lock()
	usable := m.usableLocked()
	changed := usable != m.lastUsable
	m.lastUsable = usable
	fn := m.onUsableChange
	m.mu.Unlock()
	if changed && fn != nil {
		fn(usable)
	}
}

func (m *SctpRelayManager) HasConnection() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.connections {
		if c.getState() == relayStateOpen {
			return true
		}
	}
	return false
}

func (m *SctpRelayManager) ConnectedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.connections {
		if c.getState() == relayStateOpen {
			n++
		}
	}
	return n
}

func (m *SctpRelayManager) failConnection(conn *relayConnection) {
	m.mu.Lock()
	if conn.getState() == relayStateFailed {
		m.mu.Unlock()
		return
	}
	conn.setState(relayStateFailed)
	delete(m.connections, conn.id)
	m.mu.Unlock()
	m.teardown(conn)
	m.recomputeHealth()
}

func (m *SctpRelayManager) closeConnection(id string) {
	m.mu.Lock()
	conn := m.connections[id]
	if conn == nil {
		m.mu.Unlock()
		return
	}
	conn.setState(relayStateClosed)
	delete(m.connections, id)
	m.mu.Unlock()
	m.teardown(conn)
	m.recomputeHealth()
}

func (m *SctpRelayManager) teardown(conn *relayConnection) {
	conn.teardownOnce.Do(func() {
		close(conn.stopCh)
		if conn.keepalive != nil {
			conn.keepalive.Stop()
		}
		if conn.channel != nil {
			_ = conn.channel.Close()
		}
		if conn.pc != nil {
			_ = conn.pc.Close()
		}
		if conn.mem > 0 {
			m.obs().ReleaseMem(conn.mem)
		}
	})
}

func (m *SctpRelayManager) DropConnections() {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.connections = map[string]*relayConnection{}
	m.mu.Unlock()
	for _, c := range conns {
		m.teardown(c)
	}
	m.recomputeHealth()
}

func (m *SctpRelayManager) Cleanup() {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.connections = map[string]*relayConnection{}
	m.streamSelfSsrcs = nil
	m.streamPeerSsrcs = nil
	m.lastUsable = 0
	m.mu.Unlock()
	m.audioSsrc.Store(0)
	m.subscriptionSsrc.Store(0)
	var nop core.CallObserver = core.NopObserver{}
	m.observer.Store(&nop)
	for _, c := range conns {
		m.teardown(c)
	}
}
