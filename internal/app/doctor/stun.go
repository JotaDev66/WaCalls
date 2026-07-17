package doctor

import (
	"fmt"
	"net"
	"strings"
)

func checkExternalIP(configured []string, ip net.IP, server string, err error) checkResult {
	const name = "external ip (stun)"
	if err != nil {
		if len(configured) > 0 {
			return checkResult{name: name, status: statusInfo, detail: fmt.Sprintf("stun probe failed (%v); cannot verify WACALLS_PUBLIC_IP", err)}
		}
		return checkResult{name: name, status: statusWarn, detail: fmt.Sprintf("stun probe failed (%v); cannot discover the public ip", err)}
	}
	if len(configured) == 0 {
		return checkResult{name: name, status: statusInfo, detail: fmt.Sprintf("%s (via %s); set WACALLS_PUBLIC_IP=%s to advertise it", ip, server, ip)}
	}
	for _, raw := range configured {
		if parsed := net.ParseIP(raw); parsed != nil && parsed.Equal(ip) {
			return checkResult{name: name, status: statusOK, detail: fmt.Sprintf("WACALLS_PUBLIC_IP confirmed via stun (%s)", server)}
		}
	}
	return checkResult{name: name, status: statusWarn, detail: fmt.Sprintf("stun (%s) reports %s but WACALLS_PUBLIC_IP is %s; remote peers may not reach the advertised address", server, ip, strings.Join(configured, ", "))}
}
