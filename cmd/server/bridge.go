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

// Bridge is the browser-leg adapter: it carries raw PCM (and, on a video call,
// encoded VP8 frames) between the browser and the CallManager over WebRTC data
// channels. The call core only ever sees []float32 PCM and media.VideoFrame, so
// it stays unaware of the transport.
type Bridge struct {
	pc      *webrtc.PeerConnection
	dc      atomic.Pointer[webrtc.DataChannel]
	videoDC atomic.Pointer[webrtc.DataChannel]
	log     *slog.Logger

	// OnBrowserPCM is invoked with decoded 16 kHz mono PCM captured from the browser mic.
	OnBrowserPCM func(pcm []float32)
	// OnBrowserVideo is invoked with each encoded VP8 frame captured from the browser camera.
	OnBrowserVideo func(f media.VideoFrame)
	// OnTerminalICE fires when the peer connection fails or closes.
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
				if cb == nil {
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
			if br.OnTerminalICE != nil {
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

func (b *Bridge) Close() {
	if b.pc != nil {
		_ = b.pc.Close()
	}
}
