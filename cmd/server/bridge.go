package main

import (
	"log/slog"
	"sync/atomic"
	"time"

	"wacalls/internal/voip/media"

	"github.com/pion/webrtc/v4"
	"github.com/purpshell/meowcaller/rtp"
)

// pcmChannelLabel is the data channel the browser opens to carry raw 16 kHz mono
// Int16 LE PCM in both directions. The browser side must create it with this label.
const pcmChannelLabel = "pcm"

// videoChannelLabel is the data channel the browser opens for a video call to
// carry encoded VP8 frames (WebCodecs) in both directions, each prefixed with
// media.VideoFrame's 5-byte header. Absent on audio-only calls.
const videoChannelLabel = "vp8"

// Bridge é o adaptador do lado do browser: carrega PCM cru (e, numa chamada
// de vídeo, os access units H.264 codificados) entre o browser e a Call do
// meowcaller via data channels WebRTC. O núcleo da chamada só vê []float32
// PCM e media.VideoFrame, sem saber nada do transporte.
type Bridge struct {
	pc      *webrtc.PeerConnection
	dc      atomic.Pointer[webrtc.DataChannel]
	videoDC atomic.Pointer[webrtc.DataChannel]
	log     *slog.Logger

	// quiet suprime o OnTerminalICE quando setado — ver CloseQuiet. É lido
	// pela própria goroutine de estado ICE do pion, por isso precisa ser
	// atômico em vez de um bool simples sem proteção nenhuma.
	quiet atomic.Bool

	// OnBrowserPCM é chamado com o PCM mono 16 kHz decodificado, capturado do microfone do browser.
	OnBrowserPCM func(pcm []float32)
	// OnBrowserVideo é chamado com cada quadro VP8 codificado, capturado da câmera do browser.
	OnBrowserVideo func(f media.VideoFrame)
	// OnTerminalICE dispara quando a peer connection falha ou fecha (a menos
	// que tenha fechado via CloseQuiet). Setado uma vez na criação do bridge,
	// antes dele ser exposto a qualquer outra goroutine — nunca é alterado
	// depois disso, então ler sem trava no callback de ICE abaixo é seguro.
	OnTerminalICE func()
}

func NewBridge(offerSDP string, log *slog.Logger) (*Bridge, string, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", err
	}
	br := &Bridge{pc: pc, log: log}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		switch dc.Label() {
		case pcmChannelLabel:
			br.dc.Store(dc)
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				if cb := br.OnBrowserPCM; cb != nil && len(msg.Data) > 0 {
					cb(media.PCMInt16LEToFloat32(msg.Data))
				}
			})
		case videoChannelLabel:
			br.videoDC.Store(dc)
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				cb := br.OnBrowserVideo
				if cb == nil || media.IsVideoKeyframeRequest(msg.Data) {
					return
				}
				if f, ok := media.DecodeVideoFrame(msg.Data); ok {
					cb(f)
				}
			})
		}
	})

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		log.Debug("browser ice state", "state", s.String())
		if s == webrtc.ICEConnectionStateFailed || s == webrtc.ICEConnectionStateClosed {
			if br.OnTerminalICE != nil && !br.quiet.Load() {
				br.OnTerminalICE()
			}
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		pc.Close()
		return nil, "", err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return nil, "", err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return nil, "", err
	}
	<-gatherComplete

	return br, pc.LocalDescription().SDP, nil
}

// WritePCM sends 16 kHz mono float32 PCM to the browser as Int16 LE over the data
// channel. It is a no-op until the channel is open.
func (b *Bridge) WritePCM(pcm []float32) error {
	dc := b.dc.Load()
	if dc == nil || len(pcm) == 0 {
		return nil
	}
	return dc.Send(media.PCMFloat32ToInt16LE(pcm))
}

// WriteVideo sends one encoded VP8 frame from the peer to the browser over the
// "vp8" data channel. It is a no-op until that channel is open (audio-only
// calls never open it).
func (b *Bridge) WriteVideo(f media.VideoFrame) error {
	dc := b.videoDC.Load()
	if dc == nil || len(f.Data) == 0 {
		return nil
	}
	return dc.Send(media.EncodeVideoFrame(f))
}

// RequestKeyframe asks the browser to emit an immediate keyframe, over the same
// "vp8" data channel as a 1-byte control message. Fired when the peer sends a
// RTCP PLI/FIR. No-op until the channel is open.
func (b *Bridge) RequestKeyframe() error {
	dc := b.videoDC.Load()
	if dc == nil {
		return nil
	}
	return dc.Send(media.VideoKeyframeRequestMsg())
}

// Renegotiate applies a new SDP offer to the SAME peer connection (unlike
// NewBridge, which always builds a fresh one) and returns the new answer.
// Used to add the "vp8" data channel mid-call when a browser upgrades an
// audio-only call to video — a full bridge replacement would glitch the
// already-flowing audio, so this drives a real WebRTC renegotiation instead.
func (b *Bridge) Renegotiate(offerSDP string) (string, error) {
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return "", err
	}
	answer, err := b.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	gatherComplete := webrtc.GatheringCompletePromise(b.pc)
	if err := b.pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	<-gatherComplete
	return b.pc.LocalDescription().SDP, nil
}

func (b *Bridge) Close() {
	if b.pc != nil {
		_ = b.pc.Close()
	}
}

// CloseQuiet fecha a peer connection sem disparar OnTerminalICE — para quem
// está derrubando essa perna WebRTC de propósito enquanto a chamada
// WhatsApp por baixo precisa continuar viva (pickup passando o bridge pra
// outro agente). Um Close comum aqui cascatearia num hangup de verdade, como
// se fosse uma falha de ICE iniciada pelo usuário.
func (b *Bridge) CloseQuiet() {
	b.quiet.Store(true)
	b.Close()
}

// orientedVideoSink recebe o vídeo do peer (meowcaller.VideoSink) E a
// orientação da câmera dele (meowcaller.VideoOrientationSink) — um
// meowcaller.VideoSinkFunc simples não implementa a segunda interface, então
// nunca fica sabendo quando o peer segura o celular de lado, e o vídeo
// renderiza rotacionado errado no browser (client/src/lib/video-pipe.ts gira
// o canvas com base nisso). orientation fica atômico porque SetOrientation
// roda na goroutine do motor do meowcaller, WriteVideo é chamada por ela
// também mas não custa garantir.
//
// bridge começa nil de propósito: este sink é registrado no meowcaller (via
// Call.ReceiveVideo) já em wireCall, ANTES do bridge WebRTC do browser
// existir — só assim o primeiro pacote de vídeo de uma chamada que já nasce
// em vídeo (não upgrade no meio) não perde a orientação. O motor do
// meowcaller só chama SetOrientation numa MUDANÇA de estado; se o sink não
// existisse ainda quando esse primeiro pacote chega, essa orientação real
// se perde pro resto da chamada (o bug "vídeo direto do celular fica de
// lado" — só upgrade no meio funcionava, porque aí o sink já estava
// plugado desde o começo da chamada em áudio). doWebRTC chama attachBridge
// assim que o bridge do browser fica pronto, sem trocar o sink.
type orientedVideoSink struct {
	bridge      atomic.Pointer[Bridge]
	videoStart  time.Time
	orientation atomic.Int32 // código CVO 0-3, ver rotationDegrees
}

func newOrientedVideoSink() *orientedVideoSink {
	return &orientedVideoSink{videoStart: time.Now()}
}

// attachBridge pluga o bridge WebRTC do browser assim que ele existir.
// Seguro de chamar concorrentemente com WriteVideo/SetOrientation, que já
// podem estar rodando desde antes (chamada que nasce em vídeo).
func (s *orientedVideoSink) attachBridge(b *Bridge) {
	s.bridge.Store(b)
}

// WriteVideo consome um access unit H.264 do peer (meowcaller.VideoSink).
func (s *orientedVideoSink) WriteVideo(au []byte) error {
	b := s.bridge.Load()
	if b == nil {
		return nil // bridge do browser ainda não existe — descarta
	}
	// O decoder WebCodecs do browser só arranca ao ver um quadro marcado
	// como keyframe, e precisa de um timestamp real e crescente por quadro
	// pra não travar depois do primeiro.
	return b.WriteVideo(media.VideoFrame{
		Data:        au,
		Keyframe:    rtp.AUHasIDR(au),
		TimestampMS: uint32(time.Since(s.videoStart).Milliseconds()),
		Rotation:    rotationDegrees(int(s.orientation.Load())),
	})
}

// Close é um no-op — não há recurso próprio pra liberar, o bridge cuida do
// seu próprio ciclo de vida.
func (s *orientedVideoSink) Close() error { return nil }

// SetOrientation implementa meowcaller.VideoOrientationSink — chamada
// sempre que a extensão RTP CVO do peer muda (ele girou o celular).
func (s *orientedVideoSink) SetOrientation(orientation int) {
	s.orientation.Store(int32(orientation))
}

// rotationDegrees converte o código CVO de 2 bits (0-3) do meowcaller pro
// grau em sentido horário que media.VideoFrame.Rotation espera.
func rotationDegrees(code int) uint16 {
	switch code & 0x03 {
	case 1:
		return 90
	case 2:
		return 180
	case 3:
		return 270
	default:
		return 0
	}
}
