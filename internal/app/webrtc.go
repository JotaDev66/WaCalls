package app

import (
	"os"
	"strings"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

func buildBrowserAPI(udpPort int, externalIPs []string) (*webrtc.API, error) {
	if udpPort <= 0 {
		return webrtc.NewAPI(), nil
	}

	opts := []ice.UDPMuxFromPortOption{ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4)}
	if iface := defaultRouteInterface(); iface != "" {
		opts = append(opts, ice.UDPMuxFromPortWithInterfaceFilter(func(name string) bool {
			return name == iface
		}))
	}

	mux, err := ice.NewMultiUDPMuxFromPort(udpPort, opts...)
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

func defaultRouteInterface() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	return parseDefaultRoute(string(data))
}

func parseDefaultRoute(table string) string {
	for _, line := range strings.Split(table, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[1] == "00000000" && fields[0] != "" {
			return fields[0]
		}
	}
	return ""
}
