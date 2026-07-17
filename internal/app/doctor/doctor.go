package doctor

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	"wacalls/internal/app/config"
	"wacalls/internal/store"
)

type checkStatus int

const (
	statusOK checkStatus = iota
	statusInfo
	statusWarn
	statusFail
)

func (s checkStatus) label() string {
	switch s {
	case statusOK:
		return "ok"
	case statusInfo:
		return "info"
	case statusWarn:
		return "warn"
	default:
		return "fail"
	}
}

type checkResult struct {
	name   string
	detail string
	status checkStatus
}

// Doctor runs preflight connectivity checks against cfg, writes a report to w,
// and reports whether every check passed (no fail). It is safe to run before
// the server starts; it opens and closes each resource it probes.
func Doctor(ctx context.Context, cfg config.Config, w io.Writer) bool {
	stunIP, stunServer, stunErr := probeExternalIP(ctx, cfg.STUNServers, cfg.WebRTCUDPPort)
	results := []checkResult{
		checkUDPPort(cfg.WebRTCUDPPort),
		checkPublicIP(cfg.PublicIPs, localIPv4s()),
		checkExternalIP(cfg.PublicIPs, stunIP, stunServer, stunErr),
		checkDatabase(ctx, storeConfigFor(cfg)),
		{name: "relays", status: statusInfo, detail: "discovered per call via WhatsApp signaling; not checkable in preflight"},
	}
	ok := true
	for _, r := range results {
		line := fmt.Sprintf("[%s]\t%s", r.status.label(), r.name)
		if r.detail != "" {
			line += " — " + r.detail
		}
		_, _ = io.WriteString(w, line+"\n")
		if r.status == statusFail {
			ok = false
		}
	}
	return ok
}

func checkUDPPort(port int) checkResult {
	const name = "webrtc udp port"
	if port <= 0 {
		return checkResult{name: name, status: statusInfo, detail: "ephemeral (WACALLS_WEBRTC_UDP_PORT unset); works, but a fixed port is friendlier for NAT/firewall rules"}
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return checkResult{name: name, status: statusFail, detail: fmt.Sprintf("cannot bind udp4 :%d — %v", port, err)}
	}
	_ = conn.Close()
	return checkResult{name: name, status: statusOK, detail: fmt.Sprintf("udp4 :%d bindable", port)}
}

func checkPublicIP(configured []string, local []net.IP) checkResult {
	const name = "public ip"
	if len(configured) > 0 {
		for _, raw := range configured {
			ip := net.ParseIP(raw)
			if ip == nil {
				return checkResult{name: name, status: statusFail, detail: fmt.Sprintf("WACALLS_PUBLIC_IP has an unparseable address %q", raw)}
			}
			if isPrivateIP(ip) {
				return checkResult{name: name, status: statusWarn, detail: fmt.Sprintf("WACALLS_PUBLIC_IP %s is private/loopback; remote peers cannot reach it", raw)}
			}
		}
		return checkResult{name: name, status: statusOK, detail: fmt.Sprintf("WACALLS_PUBLIC_IP set to %s", strings.Join(configured, ", "))}
	}
	for _, ip := range local {
		if !isPrivateIP(ip) {
			return checkResult{name: name, status: statusOK, detail: fmt.Sprintf("a public local address (%s) is available for host candidates", ip)}
		}
	}
	return checkResult{name: name, status: statusWarn, detail: fmt.Sprintf("WACALLS_PUBLIC_IP unset and local addresses are private (%s); ICE will advertise LAN IPs and media may fail across NAT", joinLocal(local))}
}

func checkDatabase(ctx context.Context, cfg store.Config) checkResult {
	const name = "database"
	bundle, err := store.Open(ctx, cfg)
	if err != nil {
		return checkResult{name: name, status: statusFail, detail: fmt.Sprintf("cannot open store — %v", err)}
	}
	_ = bundle.Close()
	return checkResult{name: name, status: statusOK, detail: describeStore(cfg)}
}

func storeConfigFor(cfg config.Config) store.Config {
	return store.Config{DatabaseURL: cfg.DatabaseURL, SQLitePath: cfg.DBPath}
}

func describeStore(cfg store.Config) string {
	if cfg.DatabaseURL != "" {
		return "postgres reachable and migrated"
	}
	return fmt.Sprintf("sqlite %s writable and migrated", cfg.SQLitePath)
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

func localIPv4s() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if v4 := ipnet.IP.To4(); v4 != nil {
				out = append(out, v4)
			}
		}
	}
	return out
}

func joinLocal(ips []net.IP) string {
	if len(ips) == 0 {
		return "none found"
	}
	parts := make([]string, len(ips))
	for i, ip := range ips {
		parts[i] = ip.String()
	}
	return strings.Join(parts, ", ")
}
