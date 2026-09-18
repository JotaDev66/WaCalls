package call

import (
	"context"
	"log/slog"
	"sync"
	"time"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/signaling"
	"wacalls/internal/voip/transport"
	"wacalls/internal/voip/wanode"

	"github.com/polymorfa/hypermeow/types"
)

type CallManager struct {
	sock core.VoipSocket
	log  *slog.Logger

	mu          sync.Mutex
	currentCall *CallInfo

	rtpSession  *media.RtpSession
	srtpSession *media.SrtpSession
	codec       media.Codec
	relay       RelayTransport

	selfSsrc      uint32
	peerSsrcs     []uint32
	actualPeerSet bool

	// Video plane, populated only for MediaTypeVideo calls. The browser runs
	// the H264 encode/decode (WebCodecs); the CallManager only packetizes into
	// RTP and hands inbound frames back. See callmanager_video.go.
	selfVideoSsrc uint32
	peerVideoSsrc uint32
	h264Pay       *media.H264Payloader
	h264Depay     *media.H264Depacketizer

	// Plano de RTCP/SRTCP para vídeo (SR/RR/REMB) — o WhatsApp roda estimativa
	// de banda e não sustenta o vídeo sem esse retorno. Ver callmanager_rtcp.go.
	srtcpSend      *media.SrtcpContext
	srtcpRecv      *media.SrtcpContext
	rtcpStop       chan struct{}
	videoTxPkts    uint32
	videoTxOctets  uint32
	videoTxLastSeq uint16 // seq do último pacote de vídeo enviado; ver debugTryNackSsrcCandidates
	videoRxPkts    uint32
	videoRxHighSeq uint32
	// histórico dos pacotes de vídeo já protegidos, para retransmitir quando o
	// WhatsApp pede via RTCP NACK; sem isso um keyframe fragmentado nunca fecha
	// no decodificador dele. Ver videotxhistory.go / handleVideoNack.
	videoTxHist    *videoTxHistory
	videoRtxResent uint32
	// videoTxKeyframeRequired trava a saída: enquanto true, FeedCapturedVideo
	// descarta silenciosamente todo quadro que não tenha um IDR de verdade
	// (mesmo que o navegador marque o quadro como "keyframe" — o browser às
	// vezes manda o primeiro quadro sem SPS/PPS/IDR nenhum). Sem essa trava
	// mandávamos vários quadros nonIDR antes do primeiro IDR de verdade, o
	// que deixa o decodificador do WhatsApp sem chão pra começar. Começa
	// true (call/plano de vídeo novo) e volta a true sempre que pedimos um
	// keyframe (PLI/FIR ou proativo). Ideia confirmada funcionando na lib de
	// referência github.com/purpshell/meowcaller (videoSender.keyframeRequired).
	videoTxKeyframeRequired bool
	// videoTxNextTs é o timestamp RTP (90kHz) da próxima unidade de acesso de
	// vídeo, avançado por um passo FIXO (core.WAVideoClockRate/WAVideoFrameRate)
	// por AU — NÃO o tempo real de captura do navegador (f.TimestampMS). Um
	// passo fixo casa com o que o receptor espera (visto na lib de referência
	// github.com/purpshell/meowcaller: browserVideoFrameDuration é constante).
	videoTxNextTs uint32
	// contador do loop de RTCP de vídeo (1 tick/s) — usado pra pedir um
	// keyframe novo periodicamente e proativamente (ver sendVideoRtcpTick).
	// Sem retransmissão por NACK (não dá pra decifrar o feedback "fast" do
	// WhatsApp — ver VDUMP), um keyframe perdido fica perdido; pedir um novo
	// de tempos em tempos, mesmo sem PLI/FIR, dá mais chances de um chegar
	// inteiro.
	videoRtcpTicks uint32
	// último RTP timestamp de vídeo que mandamos + quando, para extrapolar o
	// RTP timestamp do Sender Report (RFC 3550: precisa corresponder ao stream).
	lastVideoTxTS   uint32
	lastVideoTxWall time.Time

	firstPacketSent       bool
	initialTransportSent  bool
	outgoingPreacceptSent bool
	acceptedByJid         string
	debeEnabled           bool

	encodeBuf    []float32
	encodeBufPos int

	lastCaptureAt time.Time
	keepaliveStop chan struct{}

	OnStateChange func(*CallInfo)
	OnIncoming    func(*CallInfo)
	OnEnded       func(*CallInfo)
	OnPeerAudio   func([]float32)
	OnPeerVideo   func(media.VideoFrame)
	// OnKeyframeRequest é chamado quando o peer manda PLI/FIR pedindo um quadro
	// de refresh; quem liga (o servidor HTTP) avisa o navegador para emitir um
	// keyframe imediato. Pode ser nil (o client já força keyframe periódico).
	OnKeyframeRequest func()
}

func NewCallManager(sock core.VoipSocket, log *slog.Logger) *CallManager {
	if log == nil {
		log = slog.Default()
	}
	m := &CallManager{
		sock:        sock,
		log:         log,
		debeEnabled: true,
	}
	relay := transport.NewSctpRelayManager(log)
	relay.SetOnConnected(func(ip string, port int) { m.onRelayConnected() })
	relay.SetOnReceive(func(data []byte) { m.onRelayData(data) })
	m.relay = relay
	return m
}

func (m *CallManager) CurrentCall() *CallInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentCall
}

func (m *CallManager) emitState() {
	if m.OnStateChange != nil && m.currentCall != nil {
		m.OnStateChange(m.currentCall)
	}
}

func (m *CallManager) StartCall(ctx context.Context, callID string, peerJid types.JID, isVideo bool) error {
	m.mu.Lock()
	if m.currentCall != nil && !m.currentCall.IsEnded() {
		m.mu.Unlock()
		return &CallError{"a call is already in progress"}
	}

	mediaType := core.CallMediaTypeAudio
	if isVideo {
		mediaType = core.CallMediaTypeVideo
	}
	creator := m.sock.OwnLID()
	if creator.IsEmpty() {
		creator = m.sock.OwnPN()
	}
	resolved := m.sock.ResolveLIDForPN(ctx, peerJid)

	call := NewOutgoingCall(callID, resolved.String(), creator.String(), mediaType)
	callKey := media.GenerateCallKey()
	call.EncryptionKey = callKey
	m.currentCall = call
	m.initialTransportSent = false
	m.outgoingPreacceptSent = false

	selfJid := creator.String()
	m.selfSsrc = media.GenerateSecureSsrc(callID, selfJid, 0)
	m.rtpSession = media.NewWhatsAppOpusSession(m.selfSsrc)
	m.peerSsrcs = []uint32{media.GenerateSecureSsrc(callID, resolved.String(), 0)}
	m.initCodec()
	if isVideo {
		m.initVideoLocked(callID, selfJid, resolved.String())
	}
	m.mu.Unlock()

	offer, err := signaling.BuildOfferStanza(ctx, m.sock, callID, callKey, resolved, isVideo)
	if err != nil {
		return err
	}
	ackNode, err := m.sock.Query(ctx, offer)
	if err != nil {
		return err
	}

	m.mu.Lock()
	_ = m.currentCall.ApplyTransition(Transition{Type: TransitionOfferSent})
	m.emitState()
	m.mu.Unlock()

	if ackNode != nil {
		go m.HandleCallAck(context.Background(), ackNode)
	}

	m.log.Info("call offer sent", "call_id", callID, "peer", resolved.String())
	return nil
}

func (m *CallManager) AcceptCall(ctx context.Context, callID string) error {
	m.mu.Lock()
	call := m.currentCall
	if call == nil || call.CallID != callID {
		m.mu.Unlock()
		return &CallError{"no incoming call with id " + callID}
	}
	if !call.CanAccept() {
		m.mu.Unlock()
		return &CallError{"call cannot be accepted in state " + string(call.StateData.State)}
	}
	_ = call.ApplyTransition(Transition{Type: TransitionLocalAccepted})
	m.emitState()
	key := call.EncryptionKey
	peer := wanode.MustJID(call.PeerJid)
	creator := wanode.MustJID(call.CallCreator)
	isVideo := call.MediaType == core.CallMediaTypeVideo
	relayData := call.RelayData
	m.mu.Unlock()

	if key != nil {
		acceptNode, err := signaling.BuildAcceptStanza(ctx, m.sock, callID, key, peer, creator, isVideo)
		if err != nil {
			m.log.Error("build accept failed", "err", err)
		} else if err := m.sock.SendNode(ctx, acceptNode); err != nil {
			m.log.Error("accept send error", "err", err)
		}
	}

	if relayData != nil {
		m.setupIncomingMedia(call, relayData)
		m.connectRelays(relayData.Endpoints)
	} else {
		m.log.Warn("call accepted but no relay endpoints yet; media path waits for a transport message", "call_id", callID)
	}
	m.log.Info("call accepted", "call_id", callID)
	return nil
}

func (m *CallManager) setupIncomingMedia(call *CallInfo, relayData *core.RelayData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(relayData.ParticipantJids) > 0 {
		ourBase := wanode.CleanJID(m.ownCredJid())
		ourDeviceJid := ensureDeviceJid(findOurDevice(relayData.ParticipantJids, ourBase, m.ownCredJid()))
		if newSelf := media.GenerateSecureSsrc(call.CallID, ourDeviceJid, 0); newSelf != m.selfSsrc {
			m.selfSsrc = newSelf
			m.rtpSession = media.NewWhatsAppOpusSession(newSelf)
		}
		if peer := firstPeerDevice(relayData.ParticipantJids, ourBase); peer != "" {
			m.peerSsrcs = []uint32{media.GenerateSecureSsrc(call.CallID, ensureDeviceJid(peer), 0)}
			m.actualPeerSet = true
		}
	}
	m.relay.SetSubscriptionSsrc(firstSsrc(m.peerSsrcs))
	if call.MediaType == core.CallMediaTypeVideo {
		if m.h264Pay == nil {
			m.initVideoLocked(call.CallID, m.ownCredJid(), call.PeerJid)
		}
		if len(relayData.ParticipantJids) > 0 {
			ourBase := wanode.CleanJID(m.ownCredJid())
			selfDev := ensureDeviceJid(findOurDevice(relayData.ParticipantJids, ourBase, m.ownCredJid()))
			peerDev := firstPeerDevice(relayData.ParticipantJids, ourBase)
			m.refineVideoSsrcLocked(call.CallID, selfDev, peerDev)
		}
	}
	m.initSrtpKeysLocked()
}

func (m *CallManager) RejectCall(ctx context.Context, callID string, reason core.EndCallReason) error {
	m.mu.Lock()
	call := m.currentCall
	if call == nil || call.CallID != callID {
		m.mu.Unlock()
		return &CallError{"no call with id " + callID}
	}
	_ = call.ApplyTransition(Transition{Type: TransitionLocalRejected, Reason: reason})
	node := signaling.BuildRejectStanza(wanode.MustJID(call.PeerJid), call.CallID, wanode.MustJID(call.CallCreator))
	m.emitState()
	m.mu.Unlock()

	go func() { _, _ = m.sock.Query(ctx, node) }()
	m.cleanupMedia()
	return nil
}

func (m *CallManager) EndCall(ctx context.Context, reason core.EndCallReason) error {
	m.mu.Lock()
	call := m.currentCall
	if call == nil || call.IsEnded() {
		m.mu.Unlock()
		return nil
	}
	_ = call.ApplyTransition(Transition{Type: TransitionTerminated, Reason: reason})
	node := signaling.BuildTerminateStanza(wanode.MustJID(call.PeerJid), call.CallID, wanode.MustJID(call.CallCreator))
	ended := call
	m.emitState()
	m.mu.Unlock()

	go func() { _, _ = m.sock.Query(ctx, node) }()
	if m.OnEnded != nil {
		m.OnEnded(ended)
	}
	m.cleanupMedia()
	return nil
}

func (m *CallManager) ownCredJid() string {
	lid := m.sock.OwnLID()
	if !lid.IsEmpty() {
		return lid.String()
	}
	return m.sock.OwnPN().String()
}

type CallError struct{ Msg string }

func (e *CallError) Error() string { return e.Msg }
