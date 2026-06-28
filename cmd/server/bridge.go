package main

import (
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"

	"wacalls/internal/voip/media"

	"github.com/pion/webrtc/v4"
)

// pcmChannelLabel is the data channel the browser opens to carry raw 16 kHz mono
// Int16 LE PCM in both directions. The browser side must create it with this label.
const pcmChannelLabel = "pcm"

// Bridge is the browser-leg adapter: it carries raw PCM between the browser and
// the CallManager over a WebRTC data channel. The call core only ever sees
// []float32 PCM, so it stays unaware of the transport (no Opus here anymore).
type Bridge struct {
	pc  *webrtc.PeerConnection
	dc  atomic.Pointer[webrtc.DataChannel]
	log *slog.Logger

	// OnBrowserPCM is invoked with decoded 16 kHz mono PCM captured from the browser mic.
	OnBrowserPCM func(pcm []float32)
	// OnTerminalICE fires when the peer connection fails or closes.
	OnTerminalICE func()
}

// browserWebRTCAPI builds the pion API used for the browser leg. When the
// deployment sets WEBRTC_NAT_1TO1_IP / WEBRTC_UDP_PORT_MIN / WEBRTC_UDP_PORT_MAX
// it applies a 1:1 NAT mapping — so the SDP answer advertises the host's PUBLIC
// IP instead of the container's internal address — and pins ICE to the published
// UDP port. Without them it falls back to pion defaults (LAN/dev).
//
// Required behind Docker/NAT: with the default empty configuration pion only
// gathers the container's private candidates, which a remote browser can never
// reach, so the browser ICE leg stays in "checking" and audio never flows.
func browserWebRTCAPI() (*webrtc.API, error) {
	se := webrtc.SettingEngine{}
	if natIP := os.Getenv("WEBRTC_NAT_1TO1_IP"); natIP != "" {
		se.SetNAT1To1IPs([]string{natIP}, webrtc.ICECandidateTypeHost)
	}
	minStr, maxStr := os.Getenv("WEBRTC_UDP_PORT_MIN"), os.Getenv("WEBRTC_UDP_PORT_MAX")
	if minStr != "" && maxStr != "" {
		lo, errLo := strconv.ParseUint(minStr, 10, 16)
		hi, errHi := strconv.ParseUint(maxStr, 10, 16)
		if errLo == nil && errHi == nil {
			if err := se.SetEphemeralUDPPortRange(uint16(lo), uint16(hi)); err != nil {
				return nil, err
			}
		}
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(se)), nil
}

func NewBridge(offerSDP string, log *slog.Logger) (*Bridge, string, error) {
	api, err := browserWebRTCAPI()
	if err != nil {
		return nil, "", err
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", err
	}
	br := &Bridge{pc: pc, log: log}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != pcmChannelLabel {
			return
		}
		br.dc.Store(dc)
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if cb := br.OnBrowserPCM; cb != nil && len(msg.Data) > 0 {
				cb(media.PCMInt16LEToFloat32(msg.Data))
			}
		})
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

func (b *Bridge) Close() {
	if b.pc != nil {
		_ = b.pc.Close()
	}
}
