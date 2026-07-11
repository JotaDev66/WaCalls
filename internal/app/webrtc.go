package app

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

var (
	browserAPIOnce sync.Once
	browserAPI     *webrtc.API
	browserAPIErr  error
)

// browserWebRTCAPI returns a process-wide *webrtc.API for the browser-facing
// PeerConnections. When WACALLS_WEBRTC_UDP_PORT is set, all ICE traffic is
// funneled through a single fixed UDP port and host candidates are published
// with WACALLS_PUBLIC_IP, so the server is reachable behind a 1:1 NAT such as a
// Docker bridge. Without the env vars it falls back to pion's default behavior
// (ephemeral ports, interface IPs) used for local/LAN runs.
func browserWebRTCAPI() (*webrtc.API, error) {
	browserAPIOnce.Do(func() {
		port, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("WACALLS_WEBRTC_UDP_PORT")))
		browserAPI, browserAPIErr = buildBrowserAPI(port, publicIPs())
	})
	return browserAPI, browserAPIErr
}

func buildBrowserAPI(udpPort int, externalIPs []string) (*webrtc.API, error) {
	if udpPort <= 0 {
		return webrtc.NewAPI(), nil
	}

	mux, err := ice.NewMultiUDPMuxFromPort(udpPort, ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4))
	if err != nil {
		return nil, err
	}

	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(mux)
	if len(externalIPs) > 0 {
		if err := se.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        externalIPs,
			AsCandidateType: webrtc.ICECandidateTypeHost,
		}); err != nil {
			return nil, err
		}
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(se)), nil
}

func publicIPs() []string {
	raw := strings.TrimSpace(os.Getenv("WACALLS_PUBLIC_IP"))
	if raw == "" {
		return nil
	}
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
