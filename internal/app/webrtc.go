package app

import (
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

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
