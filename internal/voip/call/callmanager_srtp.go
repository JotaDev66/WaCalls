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
	m.log.Debug("srtp re-initialized with peer call key")
}
