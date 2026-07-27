package call

import (
	"context"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/signaling"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func (m *CallManager) HandleCallOffer(ctx context.Context, node *waBinary.Node, peerJid types.JID, callKey []byte) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	callID := info.CallID
	creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
	if creator == "" {
		creator = peerJid.String()
	}
	relays := signaling.ExtractRelayEndpoints(info.InnerNode)
	var structured *signaling.ParsedRelayAck
	if len(relays) == 0 {
		// The offer may carry relays in the structured <relay><te2> form (the
		// same encoding acks use) rather than as <relay ip=.. token=..>
		// attributes. ExtractRelayEndpoints only reads the attribute form, so
		// fall back to the structured parser before giving up.
		if parsed := signaling.ParseRelayFromAck(info.InnerNode); len(parsed.Relays) > 0 {
			relays = parsed.Relays
			structured = &parsed
			m.log.Info("offer relays parsed via structured (te2) format", "call_id", callID, "relays", len(relays))
		}
	}
	// Diagnostic: show the offer's child structure so we can see whether relays
	// are present (and in which form) or genuinely arrive later.
	m.log.Debug("offer inner node structure", "call_id", callID, "children", childTagSummary(info.InnerNode))

	mediaType := core.CallMediaTypeAudio

	m.mu.Lock()
	call := NewIncomingCall(callID, peerJid.String(), creator, "", mediaType)
	if callKey != nil {
		call.EncryptionKey = callKey
	}
	if len(relays) > 0 {
		rd := &core.RelayData{Endpoints: relays}
		if structured != nil {
			// Carry the full structured data so SRTP/SSRC setup has participants.
			rd.ParticipantJids = structured.ParticipantJids
			rd.UUID = structured.UUID
			rd.SelfPid = structured.SelfPid
			rd.PeerPid = structured.PeerPid
			rd.HbhKey = structured.HbhKey
		}
		call.RelayData = rd
	}
	m.currentCall = call
	m.initialTransportSent = false

	selfJid := m.sock.OwnLID()
	sj := selfJid.String()
	if selfJid.IsEmpty() {
		sj = m.sock.OwnPN().String()
	}
	m.selfSsrc = media.GenerateSecureSsrc(callID, sj, 0)
	m.replaceRtpSession(media.NewWhatsAppOpusSession(m.selfSsrc))
	m.peerSsrcs = []uint32{media.GenerateSecureSsrc(callID, peerJid.String(), 0)}
	m.mu.Unlock()

	m.applyVoipSettings(info.InnerNode, callID)

	preaccept := signaling.BuildPreacceptStanza(peerJid, callID, wanode.MustJID(creator))
	if err := m.sock.SendNode(ctx, preaccept); err != nil {
		m.log.Error("send preaccept", "err", err)
	}

	if m.OnIncoming != nil {
		m.OnIncoming(call)
	}
	m.mu.Lock()
	m.emitState()
	m.mu.Unlock()
	m.startWatchdog()
	m.log.Info("incoming call", "call_id", callID, "peer", peerJid.String(), "relays", len(relays))
}

func (m *CallManager) HandleCallAccept(ctx context.Context, node *waBinary.Node, peerJid types.JID) {
	m.mu.Lock()
	call := m.currentCall
	// First accept wins: a later accept from a SIBLING device must not swap
	// acceptedByJid and rekey SRTP under an established media path. A retry from
	// the same device passes through: its first accept may have carried a call key
	// we could not decrypt (signal-session desync), and the retransmission is the
	// only chance to repair the keying.
	if accepted := m.acceptedByJid; accepted != "" && accepted != peerJid.String() {
		m.mu.Unlock()
		m.log.Info("accept from another device ignored", "accepted_by", accepted, "from", peerJid.String())
		return
	}
	m.mu.Unlock()
	if call == nil {
		return
	}
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}

	if signaling.NeedsDecryption(info.Tag) {
		peerKey, err := signaling.DecryptCallKeyInNode(ctx, m.sock, info.InnerNode, peerJid)
		if err != nil {
			m.log.Warn("accept call key undecryptable; skipping rekey", "call_id", call.CallID, "err", err)
		} else if peerKey != nil {
			m.mu.Lock()
			if !call.IsEnded() && call.EncryptionKey != nil && !equalBytes(call.EncryptionKey, peerKey) {
				m.reinitSrtpLocked(peerKey, peerJid)
			}
			m.mu.Unlock()
		}
	}

	m.mu.Lock()
	// The call can be torn down (EndCall/terminate) during the unlocked decrypt window above;
	// re-arming SRTP/SRTCP and relays on an ended call would leak contexts and connections and
	// let stale keying bleed into the next call.
	if call.IsEnded() {
		m.mu.Unlock()
		return
	}
	_ = call.ApplyTransition(Transition{Type: TransitionRemoteAccepted})
	m.emitState()
	firstAccept := m.acceptedByJid == ""
	m.acceptedByJid = peerJid.String()
	if m.peerSsrcs == nil || !m.actualPeerSet {
		peerDeviceJid := ensureDeviceJid(peerJid.String())
		m.peerSsrcs = []uint32{media.GenerateSecureSsrc(call.CallID, peerDeviceJid, 0)}
	}
	m.relay.SetSubscriptionSsrc(firstSsrc(m.peerSsrcs))
	m.initSrtpKeysLocked()
	hasConn := m.relay.HasConnection()
	relayData := call.RelayData
	var siblings []types.JID
	if firstAccept {
		for _, dev := range m.calleeDevices {
			if dev.String() != peerJid.String() {
				siblings = append(siblings, dev)
			}
		}
	}
	basePeer := call.PeerJid
	m.mu.Unlock()

	m.log.Info("remote accepted call", "call_id", call.CallID, "peer", peerJid.String(),
		"relay_connected", hasConn, "relay_endpoints", relayEndpointCount(relayData))

	m.relay.ResendSubscriptions()

	callID := call.CallID
	creator := wanode.MustJID(call.CallCreator)
	if len(siblings) > 0 {
		elsewhere := signaling.BuildTerminateElsewhereStanza(wanode.MustJID(basePeer), callID, creator, siblings)
		if err := m.sock.SendNode(ctx, elsewhere); err != nil {
			m.log.Warn("accepted_elsewhere fanout failed; sibling devices may keep ringing",
				"call_id", callID, "err", err)
		} else {
			m.log.Info("accepted_elsewhere sent to non-answering devices", "call_id", callID, "devices", len(siblings))
		}
	}
	m.sendTransportUpdate(ctx, peerJid, creator, callID)
	_ = m.sock.SendNode(ctx, signaling.BuildMuteV2Stanza(peerJid, callID, creator, 0))
	if acceptMsgID := wanode.AttrString(node.Attrs, "id"); acceptMsgID != "" {
		ourJid := m.sock.OwnLID()
		if ourJid.IsEmpty() {
			ourJid = m.sock.OwnPN()
		}
		_ = m.sock.SendNode(ctx, signaling.BuildAcceptReceiptStanza(peerJid, acceptMsgID, callID, creator, ourJid))
	}

	if hasConn {
		m.mu.Lock()
		if err := call.ApplyTransition(Transition{Type: TransitionMediaConnected}); err == nil {
			m.emitState()
			m.maybeStartRtcpTxLocked()
			m.log.Info("call ACTIVE (media path established)", "call_id", call.CallID)
		}
		m.mu.Unlock()
	} else if relayData != nil {
		m.connectRelays(relayData.Endpoints)
	}
}

func (m *CallManager) sendTransportUpdate(ctx context.Context, peer, creator types.JID, callID string) {
	transport := waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"to": peer, "id": signaling.GenerateCallStanzaID()},
		Content: []waBinary.Node{{
			Tag: "transport",
			Attrs: waBinary.Attrs{
				"call-id": callID, "call-creator": creator,
				"transport-message-type": "1", "p2p-cand-round": "1",
			},
			Content: []waBinary.Node{{Tag: "net", Attrs: waBinary.Attrs{"medium": "2", "protocol": "0"}}},
		}},
	}
	_ = m.sock.SendNode(ctx, transport)
}

func (m *CallManager) HandleCallTransport(ctx context.Context, node *waBinary.Node, peerJid types.JID) {
	m.mu.Lock()
	call := m.currentCall
	m.mu.Unlock()
	if call == nil {
		return
	}
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	relays := signaling.ExtractRelayEndpoints(info.InnerNode)
	var structured *signaling.ParsedRelayAck
	if len(relays) == 0 {
		if parsed := signaling.ParseRelayFromNode(info.InnerNode); len(parsed.Relays) > 0 {
			relays = parsed.Relays
			structured = &parsed
			m.log.Info("transport relays parsed via structured (te2) format", "call_id", call.CallID, "relays", len(relays))
		}
	}
	m.log.Info("call transport received", "call_id", call.CallID,
		"relays", len(relays), "already_connected", m.relay.HasConnection(),
		"type", wanode.AttrString(info.InnerNode.Attrs, "transport-message-type"),
		"children", childTagSummary(info.InnerNode))
	if len(relays) == 0 || m.relay.HasConnection() {
		return
	}
	if len(buildRelayConfigs(relays)) == 0 {
		m.log.Warn("transport relays not dialable; keeping stored endpoints", "call_id", call.CallID)
		return
	}
	m.mu.Lock()
	if call.RelayData == nil {
		call.RelayData = &core.RelayData{}
	}
	call.RelayData.Endpoints = relays
	if structured != nil {
		if call.RelayData.HbhKey == nil {
			call.RelayData.HbhKey = structured.HbhKey
		}
		if len(call.RelayData.ParticipantJids) == 0 {
			call.RelayData.ParticipantJids = structured.ParticipantJids
		}
		if call.RelayData.UUID == "" {
			call.RelayData.UUID = structured.UUID
		}
		if call.RelayData.SelfPid == nil {
			call.RelayData.SelfPid = structured.SelfPid
		}
		if call.RelayData.PeerPid == nil {
			call.RelayData.PeerPid = structured.PeerPid
		}
	}
	m.mu.Unlock()
	m.connectRelays(relays)
}

func (m *CallManager) HandleCallRelayLatency(ctx context.Context, node *waBinary.Node, peerJid types.JID) {
	m.mu.Lock()
	call := m.currentCall
	m.mu.Unlock()
	if call == nil {
		return
	}
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	m.mu.Lock()
	if call.RelayData == nil || len(call.RelayData.Endpoints) == 0 {
		if parsed := signaling.ParseRelayFromNode(info.InnerNode); len(parsed.Relays) > 0 {
			call.RelayData = &core.RelayData{
				Endpoints: parsed.Relays, ParticipantJids: parsed.ParticipantJids,
				UUID: parsed.UUID, SelfPid: parsed.SelfPid, PeerPid: parsed.PeerPid, HbhKey: parsed.HbhKey,
			}
			m.log.Info("relay data harvested from relaylatency", "call_id", call.CallID, "relays", len(parsed.Relays))
		}
	}
	incoming := call.Direction == core.CallDirectionIncoming
	creator := call.CallCreator
	callID := call.CallID
	m.mu.Unlock()
	if !incoming {
		return
	}
	creatorJid := wanode.MustJID(creator)
	echoed := 0
	for _, te := range wanode.NodeChildren(info.InnerNode) {
		if te.Tag != "te" {
			continue
		}
		relayName := wanode.AttrString(te.Attrs, "relay_name")
		if relayName == "" {
			continue
		}
		entry := signaling.RelayLatencyEntry{
			RelayName:    relayName,
			Latency:      signaling.DecodeLatency(wanode.AttrString(te.Attrs, "latency")),
			AddressBytes: wanode.NodeBytes(&te),
		}
		echo := signaling.BuildRelayLatencyStanza(peerJid, callID, creatorJid, []signaling.RelayLatencyEntry{entry}, nil)
		if err := m.sock.SendNode(ctx, echo); err != nil {
			m.log.Debug("relaylatency echo send failed", "call_id", callID, "err", err)
			return
		}
		echoed++
	}
	if echoed > 0 {
		m.log.Info("relaylatency probes echoed", "call_id", callID, "probes", echoed)
	}
}

func (m *CallManager) HandleCallAck(ctx context.Context, node *waBinary.Node) {
	if t := wanode.AttrString(node.Attrs, "type"); t != "offer" {
		return
	}
	if e := wanode.AttrString(node.Attrs, "error"); e != "" {
		m.log.Error("offer ack error", "error", e)
		return
	}
	parsed := signaling.ParseRelayFromAck(node)
	m.log.Info("offer ack received", "relays", len(parsed.Relays), "participants", len(parsed.ParticipantJids))
	if len(parsed.Relays) == 0 {
		return
	}

	m.mu.Lock()
	call := m.currentCall
	if call == nil {
		m.mu.Unlock()
		return
	}
	call.RelayData = &core.RelayData{
		Endpoints:       parsed.Relays,
		ParticipantJids: parsed.ParticipantJids,
		UUID:            parsed.UUID,
		SelfPid:         parsed.SelfPid,
		PeerPid:         parsed.PeerPid,
		HbhKey:          parsed.HbhKey,
	}

	ourBase := wanode.CleanJID(m.ownCredJid())
	if len(parsed.ParticipantJids) > 0 {
		ourDeviceJid := ensureDeviceJid(findOurDevice(parsed.ParticipantJids, ourBase, m.ownCredJid()))
		newSelf := media.GenerateSecureSsrc(call.CallID, ourDeviceJid, 0)
		if newSelf != m.selfSsrc {
			m.selfSsrc = newSelf
			m.replaceRtpSession(media.NewWhatsAppOpusSession(newSelf))
		}
		if peer := firstPeerDevice(parsed.ParticipantJids, ourBase); peer != "" {
			m.peerSsrcs = []uint32{media.GenerateSecureSsrc(call.CallID, ensureDeviceJid(peer), 0)}
		}
		if call.EncryptionKey != nil {
			m.initSrtpKeysLocked()
		}
	}
	isInitiator := call.IsInitiator()
	peer := wanode.MustJID(call.PeerJid)
	callID := call.CallID
	creator := wanode.MustJID(call.CallCreator)
	sendPreaccept := isInitiator && !m.outgoingPreacceptSent
	if sendPreaccept {
		m.outgoingPreacceptSent = true
	}
	endpoints := parsed.Relays
	m.mu.Unlock()

	m.applyVoipSettings(node, callID)

	if sendPreaccept {
		_ = m.sock.SendNode(ctx, signaling.BuildPreacceptStanza(peer, callID, creator))
	}
	m.connectRelays(endpoints)
}

func (m *CallManager) HandleCallTerminate(node *waBinary.Node) {
	m.mu.Lock()
	call := m.currentCall
	if call == nil {
		m.mu.Unlock()
		return
	}
	// After an accept, only the answering device may end the call. A sibling that
	// kept ringing eventually times out and sends its own reject/terminate, which
	// must not tear down the live call.
	sender := wanode.AttrString(node.Attrs, "from")
	if m.acceptedByJid != "" && sender != "" && sender != m.acceptedByJid && !call.IsEnded() {
		m.mu.Unlock()
		m.log.Info("terminate from non-answering device ignored",
			"call_id", call.CallID, "from", sender, "accepted_by", m.acceptedByJid)
		return
	}
	info := signaling.ExtractNodeInfo(node)
	reason := core.EndCallReasonUserEnded
	if info != nil {
		if r := wanode.AttrString(info.InnerNode.Attrs, "reason"); r != "" {
			reason = core.EndCallReason(r)
		}
	}
	m.log.Info("call terminated by peer", "call_id", call.CallID, "reason", string(reason))
	_ = call.ApplyTransition(Transition{Type: TransitionTerminated, Reason: reason})
	ended := call
	m.emitState()
	m.mu.Unlock()

	if m.OnEnded != nil {
		m.OnEnded(ended)
	}
	m.cleanupMedia()
}

// applyVoipSettings records the codec the server selected for the call from the
// <voip_settings> blob (inbound offer or outbound offer ack). Absent or malformed
// settings keep the MLow default. Decode of inbound standard Opus is already
// handled per-frame by the codec fallback; the Warn flags that our MLow encode may
// not be decodable by the peer.
func (m *CallManager) applyVoipSettings(node *waBinary.Node, callID string) {
	vsNode := wanode.FindChildByTag(node, "voip_settings")
	if vsNode == nil {
		return
	}
	vs, err := signaling.ParseVoipSettings(wanode.NodeBytes(vsNode))
	if err != nil {
		m.log.Debug("voip_settings parse failed; keeping mlow", "call_id", callID, "err", err)
		return
	}
	codec := vs.CodecName()
	m.mu.Lock()
	if call := m.currentCall; call != nil && call.CallID == callID {
		call.Codec = codec
	}
	m.mu.Unlock()
	m.log.Info("voip_settings parsed", "call_id", callID, "codec", codec,
		"use_mlow_codec_v1", vs.UseMlowCodecV1, "frame_ms", vs.FrameMs, "target_bitrate", vs.TargetBitrate)
	if codec == signaling.CodecOpus {
		m.log.Warn("server selected standard opus; wacalls encodes mlow and the peer may not decode our audio",
			"call_id", callID)
	}
}

func (m *CallManager) HandleCallMute(node *waBinary.Node) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil || info.Tag != "mute_v2" {
		return
	}
	m.mu.Lock()
	call := m.currentCall
	live := call != nil && call.CallID == info.CallID && !call.IsEnded()
	m.mu.Unlock()
	if !live {
		return
	}
	muted := wanode.AttrString(info.InnerNode.Attrs, "mute-state") == "1"
	if m.OnPeerMute != nil {
		m.OnPeerMute(info.CallID, muted)
	}
}
