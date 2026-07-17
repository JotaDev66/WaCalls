package call

import (
	"context"
	"maps"
	"runtime/pprof"
	"slices"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
	"wacalls/internal/voip/wanode"

	"go.mau.fi/whatsmeow/types"
)

type RelayTransport interface {
	SetSsrc(ssrc uint32)
	SetSubscriptionSsrc(ssrc uint32)
	SetStreamSsrcs(selfSsrcs, peerSsrcs []uint32)
	SetOnConnected(fn func(ip string, port int))
	SetOnReceive(fn func(data []byte))
	SetOnUsableChange(fn func(usable int))
	SetObserver(o core.CallObserver)
	ResendSubscriptions()
	ConfigureRelays(relays []transport.RelayConfig)
	DropConnections()
	Broadcast(data []byte)
	BufferedAmount() uint64
	HasConnection() bool
	ConnectedCount() int
	Cleanup()
}

var _ RelayTransport = (*transport.SctpRelayManager)(nil)

func (m *CallManager) onRelayConnected(ip string, port int) {
	m.mu.Lock()
	call := m.currentCall
	if call != nil && call.StateData.State == core.CallStateConnecting {
		if err := call.ApplyTransition(Transition{Type: TransitionMediaConnected}); err == nil {
			m.emitState()
			m.maybeStartRtcpTxLocked()
			m.log.Info("relay connected → active", "call_id", call.CallID)
		}
	}
	callID, relayName, rttMs, hasRtt := "", "", 0, false
	if call != nil && call.RelayData != nil {
		callID = call.CallID
		relayName = ip
		for _, ep := range call.RelayData.Endpoints {
			if ep.IP == ip && (port == 0 || ep.Port == port) {
				if ep.RelayName != "" {
					relayName = ep.RelayName
				}
				if ep.C2RRtt != nil {
					rttMs, hasRtt = *ep.C2RRtt, true
				}
				break
			}
		}
	}
	m.mu.Unlock()
	if m.OnRelay != nil && callID != "" {
		go m.OnRelay(callID, relayName, rttMs, hasRtt)
	}
}

func (m *CallManager) onRelayUsableChange(usable int) {
	if usable > 0 {
		m.mu.Lock()
		call := m.currentCall
		reconnecting := call != nil && call.StateData.State == core.CallStateReconnecting
		var peer, creator types.JID
		callID := ""
		if reconnecting {
			peer = wanode.MustJID(call.PeerJid)
			creator = wanode.MustJID(call.CallCreator)
			callID = call.CallID
			m.log.Info("relay transport recovered; waiting for peer media", "call_id", callID)
		}
		m.mu.Unlock()
		if reconnecting {
			m.relay.ResendSubscriptions()
			notifyDone := m.observer.TrackGoroutine()
			go func() {
				defer notifyDone()
				m.sendTransportUpdate(context.Background(), peer, creator, callID)
			}()
		}
		return
	}

	m.mu.Lock()
	call := m.currentCall
	var endpoints []core.RelayEndpoint
	if call != nil && call.StateData.State == core.CallStateActive {
		if err := call.ApplyTransition(Transition{Type: TransitionMediaLost}); err == nil {
			m.emitState()
			if call.RelayData != nil {
				endpoints = call.RelayData.Endpoints
			}
			m.log.Warn("media path lost; waiting for recovery", "call_id", call.CallID)
		}
	}
	m.mu.Unlock()

	if len(endpoints) > 0 {
		redialDone := m.observer.TrackGoroutine()
		go func() {
			defer redialDone()
			m.connectRelays(endpoints)
		}()
	}
}

func (m *CallManager) forceMediaLost() {
	m.mu.Lock()
	call := m.currentCall
	var endpoints []core.RelayEndpoint
	lost := false
	if call != nil && call.StateData.State == core.CallStateActive {
		if err := call.ApplyTransition(Transition{Type: TransitionMediaLost}); err == nil {
			m.emitState()
			lost = true
			m.lastRedialAt = time.Now()
			if call.RelayData != nil {
				endpoints = call.RelayData.Endpoints
			}
			m.log.Warn("media inactivity; recycling relay connections", "call_id", call.CallID)
		}
	}
	m.mu.Unlock()
	if !lost {
		return
	}
	m.relay.DropConnections()
	if len(endpoints) > 0 {
		redialDone := m.observer.TrackGoroutine()
		go func() {
			defer redialDone()
			m.connectRelays(endpoints)
		}()
	}
}

const redialRetryInterval = 6 * time.Second

func (m *CallManager) retryReconnect() {
	m.mu.Lock()
	call := m.currentCall
	if call == nil || call.StateData.State != core.CallStateReconnecting || time.Since(m.lastRedialAt) < redialRetryInterval {
		m.mu.Unlock()
		return
	}
	m.lastRedialAt = time.Now()
	var endpoints []core.RelayEndpoint
	if call.RelayData != nil {
		endpoints = call.RelayData.Endpoints
	}
	callID := call.CallID
	m.mu.Unlock()
	if len(endpoints) == 0 {
		return
	}
	m.log.Info("reconnect retry; recycling relay connections", "call_id", callID)
	m.relay.DropConnections()
	redialDone := m.observer.TrackGoroutine()
	go func() {
		defer redialDone()
		m.connectRelays(endpoints)
	}()
}

// prioritizeFNA returns the endpoints with the is_fna=1 relays moved to the front,
// preserving the RTT order among the rest. An inbound call's peer uplink RTP only
// arrives on the relay the offer marked is_fna=1; with maxDialRelays capping the
// dialed set, that endpoint must never be sorted out of it.
func prioritizeFNA(endpoints []core.RelayEndpoint) []core.RelayEndpoint {
	out := make([]core.RelayEndpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep.IsFNA {
			out = append(out, ep)
		}
	}
	if len(out) == 0 {
		return endpoints
	}
	for _, ep := range endpoints {
		if !ep.IsFNA {
			out = append(out, ep)
		}
	}
	return out
}

func buildRelayConfigs(endpoints []core.RelayEndpoint) []transport.RelayConfig {
	seen := map[string]bool{}
	var relays []transport.RelayConfig
	for _, ep := range endpoints {
		if ep.Protocol != 0 {
			continue
		}
		if ep.Key == "" || ep.RawToken == nil {
			continue
		}
		key := ep.IP
		if seen[key] {
			continue
		}
		seen[key] = true
		name := ep.RelayName
		if name == "" {
			name = ep.IP
		}
		relays = append(relays, transport.RelayConfig{
			IP: ep.IP, Port: ep.Port, Token: ep.Token, AuthToken: ep.AuthToken,
			RawAuthToken: ep.RawAuthToken, RawToken: ep.RawToken, Key: ep.Key,
			RelayID: ep.RelayID, Name: name, AuthTokenID: ep.AuthTokenID,
		})
	}
	return relays
}

func (m *CallManager) connectRelays(endpoints []core.RelayEndpoint) {
	m.mu.Lock()
	incoming := m.currentCall != nil && m.currentCall.Direction == core.CallDirectionIncoming
	m.mu.Unlock()
	if incoming {
		endpoints = prioritizeFNA(endpoints)
	}
	relays := buildRelayConfigs(endpoints)
	if len(relays) == 0 {
		m.log.Error("no usable relay configs")
		return
	}
	m.mu.Lock()
	callID := ""
	if m.currentCall != nil {
		callID = m.currentCall.CallID
	}
	m.relay.SetSsrc(m.selfSsrc)
	m.relay.SetSubscriptionSsrc(firstSsrc(m.peerSsrcs))
	m.mu.Unlock()
	m.relay.SetObserver(m.observer)
	pprof.Do(context.Background(), pprof.Labels("call_id", callID), func(context.Context) {
		m.relay.ConfigureRelays(relays)
	})
	m.log.Info("relay configured", "connected", m.relay.ConnectedCount())
}

func (m *CallManager) cleanupMedia() {
	m.mu.Lock()
	endResult, endReason := "", ""
	if !m.observerEnded {
		m.observerEnded = true
		if c := m.currentCall; c != nil {
			endResult = "failed"
			if c.StateData.ConnectedAt != nil {
				endResult = "completed"
			}
			endReason = string(c.StateData.EndReason)
		}
	}
	callID, codec := "", ""
	if c := m.currentCall; c != nil {
		callID = c.CallID
		codec = c.Codec
	}
	if m.srtp != nil {
		m.srtp.Close()
	}
	m.replaceRtpSession(nil)
	m.srtp = nil
	m.firstPacketSent = false
	m.initialTransportSent = false
	m.outgoingPreacceptSent = false
	m.actualPeerSet = false
	m.extAttached = false
	m.lastMediaRecv.Store(0)
	if m.watchdogStop != nil {
		close(m.watchdogStop)
		m.watchdogStop = nil
	}
	if m.rtcpTxStop != nil {
		close(m.rtcpTxStop)
		m.rtcpTxStop = nil
	}
	var qArgs []any
	if m.recvStats != nil {
		q := m.recvStats.QualitySnapshot(uint64(time.Now().UnixMilli()))
		qArgs = []any{"call_id", callID, "codec", codec, "jitter_ms", q.JitterMs, "loss", q.LossFraction, "rtt_samples", m.recvStats.RttSamples()}
		if q.HasRtt {
			qArgs = append(qArgs, "rtt_ms", q.RttMs)
		}
	}
	m.sendSrtcp = nil
	m.recvSrtcp = nil
	m.recvStats = nil
	m.rtcpCName = ""
	m.srtcpTxIndex = 0
	m.rtpPacketsSent = 0
	m.rtpOctetsSent = 0
	m.lastRtpTs = 0
	m.mu.Unlock()

	if drops := m.srtpDrops.snapshotAndReset(); len(drops) > 0 {
		args := []any{"call_id", callID}
		for _, r := range slices.Sorted(maps.Keys(drops)) {
			args = append(args, r, drops[r])
		}
		m.log.Warn("srtp recv drops summary", args...)
	}

	if drops := m.srtcpDrops.snapshotAndReset(); len(drops) > 0 {
		args := []any{"call_id", callID}
		for _, r := range slices.Sorted(maps.Keys(drops)) {
			args = append(args, r, drops[r])
		}
		m.log.Warn("srtcp recv drops summary", args...)
	}

	if qArgs != nil {
		m.log.Info("call quality summary", qArgs...)
	}

	if endResult != "" {
		m.observer.End(endResult, endReason)
	}

	m.extMu.Lock()
	m.rtpHandlers = map[uint8]func(*media.RtpPacket){}
	m.declaredSelf = map[uint32]bool{}
	m.extMu.Unlock()

	for _, e := range m.extensions {
		e.Detach()
	}
	m.relay.Cleanup()
}
