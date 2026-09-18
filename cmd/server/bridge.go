package main

import (
	"log/slog"
	"sync/atomic"

	"wacalls/internal/voip/media"

	"github.com/pion/webrtc/v4"
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
