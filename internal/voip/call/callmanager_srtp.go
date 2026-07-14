package call

import (
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/wanode"

	"go.mau.fi/whatsmeow/types"
)

func (m *CallManager) initSrtpKeysLocked() {
	call := m.currentCall
	if call == nil || call.EncryptionKey == nil {
		return
	}
	ourBase := wanode.CleanJID(m.ownCredJid())
	var participants []string
	if call.RelayData != nil {
		participants = call.RelayData.ParticipantJids
	}
	ourDeviceJid := ensureDeviceJid(findOurDevice(participants, ourBase, m.ownCredJid()))

	rawPeer := m.acceptedByJid
	if rawPeer == "" {
		rawPeer = call.PeerJid
		if p := firstPeerDevice(participants, ourBase); p != "" {
			rawPeer = p
		}
	}
	peerDeviceJid := ensureDeviceJid(rawPeer)

	sendKM, err1 := media.DerivePerJidSrtpKey(call.EncryptionKey, ourDeviceJid)
	recvKM, err2 := media.DerivePerJidSrtpKey(call.EncryptionKey, peerDeviceJid)
	if err1 != nil || err2 != nil {
		m.log.Error("srtp key derivation failed", "err1", err1, "err2", err2)
		return
	}
	m.srtp = engine.NewSrtpManager(sendKM, recvKM, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	m.srtp.SetObserver(m.observer)
	m.setupSrtcpLocked(sendKM, recvKM)
	m.log.Debug("srtp per-jid keys set", "send", ourDeviceJid, "recv", peerDeviceJid)

	m.ensureExtensionsAttachedLocked(ourDeviceJid, peerDeviceJid)
}

func (m *CallManager) reinitSrtpLocked(peerKey []byte, peerJid types.JID) {
	call := m.currentCall
	if call == nil || call.EncryptionKey == nil {
		return
	}
	ourBase := wanode.CleanJID(m.ownCredJid())
	var participants []string
	if call.RelayData != nil {
		participants = call.RelayData.ParticipantJids
	}
	ourDeviceJid := ensureDeviceJid(findOurDevice(participants, ourBase, m.ownCredJid()))
	sendKM, err1 := media.DerivePerJidSrtpKey(call.EncryptionKey, ourDeviceJid)
	recvKM, err2 := media.DerivePerJidSrtpKey(peerKey, peerJid.String())
	if err1 != nil || err2 != nil {
		return
	}
	m.srtp = engine.NewSrtpManager(sendKM, recvKM, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	m.srtp.SetObserver(m.observer)
	m.setupSrtcpLocked(sendKM, recvKM)
	m.log.Debug("srtp re-initialized with peer call key")
}

// setupSrtcpLocked derives the SRTCP contexts from the same per-jid master keying as the audio SRTP
// (labels differ inside media.NewSrtcpContext): sendKM protects our outbound RTCP, recvKM decodes
// the peer's inbound RTCP. It also readies the receiver stats that feed our RTCP report blocks and
// the quality telemetry. Caller holds m.mu.
func (m *CallManager) setupSrtcpLocked(sendKM, recvKM core.SrtpKeyingMaterial) {
	sendSrtcp, err := media.NewSrtcpContext(sendKM)
	if err != nil {
		m.log.Error("srtcp context derivation failed", "err", err)
		return
	}
	m.sendSrtcp = sendSrtcp
	recvSrtcp, err := media.NewSrtcpContext(recvKM)
	if err != nil {
		m.log.Error("srtcp recv context derivation failed", "err", err)
		return
	}
	m.recvSrtcp = recvSrtcp
	if m.recvStats == nil {
		m.recvStats = media.NewRTCPReceiverStats()
	}
	if m.rtcpCName == "" {
		m.rtcpCName = rtcpCName(m.selfSsrc)
	}
}
