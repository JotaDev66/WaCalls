package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

var stunProbeTimeout = 3 * time.Second

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

func probeExternalIP(ctx context.Context, servers []string, port int) (net.IP, string, error) {
	if len(servers) == 0 {
		return nil, "", errors.New("no stun servers configured")
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil && port > 0 {
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{})
	}
	if err != nil {
		return nil, "", fmt.Errorf("cannot bind udp socket: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var failures []string
	for _, server := range servers {
		ip, err := querySTUNServer(ctx, conn, server)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", server, err))
			continue
		}
		return ip, server, nil
	}
	return nil, "", errors.New(strings.Join(failures, "; "))
}

func querySTUNServer(ctx context.Context, conn *net.UDPConn, server string) (net.IP, error) {
	addr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return nil, err
	}
	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := conn.WriteToUDP(req.Raw, addr); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(stunProbeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return nil, err
		}
		if !stun.IsMessage(buf[:n]) {
			continue
		}
		resp := new(stun.Message)
		resp.Raw = append([]byte(nil), buf[:n]...)
		if err := resp.Decode(); err != nil {
			continue
		}
		if resp.TransactionID != req.TransactionID {
			continue
		}
		var mapped stun.XORMappedAddress
		if err := mapped.GetFrom(resp); err != nil {
			return nil, err
		}
		return mapped.IP, nil
	}
}
