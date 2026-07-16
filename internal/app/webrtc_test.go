package app

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestBuildBrowserAPIDefault(t *testing.T) {
	api, err := buildBrowserAPI(0, nil)
	if err != nil || api == nil {
		t.Fatalf("default api: got (%v, %v)", api, err)
	}
}

// TestBuildBrowserAPIMux proves the core Docker requirement: with a fixed UDP
// port and a public IP, the gathered SDP advertises a host candidate carrying
// that exact IP and port, i.e. what the browser will dial through the 1:1 NAT.
func TestBuildBrowserAPIMux(t *testing.T) {
	port := freeUDPPort(t)
	const publicIP = "203.0.113.10"

	api, err := buildBrowserAPI(port, []string{publicIP})
	if err != nil {
		t.Fatalf("buildBrowserAPI: %v", err)
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer func() { _ = pc.Close() }()

	if _, err := pc.CreateDataChannel("pcm", nil); err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-gatherComplete

	sdp := pc.LocalDescription().SDP
	if !strings.Contains(sdp, publicIP) {
		t.Fatalf("SDP missing public IP %q:\n%s", publicIP, sdp)
	}
	if !strings.Contains(sdp, " "+strconv.Itoa(port)+" typ host") {
		t.Fatalf("SDP missing host candidate on port %d:\n%s", port, sdp)
	}
}

func TestParseDefaultRoute(t *testing.T) {
	const table = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth2\t00000000\t010012AC\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"eth1\t0001000A\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t0008000A\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"
	if got := parseDefaultRoute(table); got != "eth2" {
		t.Fatalf("parseDefaultRoute = %q, want eth2", got)
	}
	if got := parseDefaultRoute("Iface\tDestination\tGateway\n"); got != "" {
		t.Fatalf("no default route: parseDefaultRoute = %q, want empty", got)
	}
	if got := parseDefaultRoute(""); got != "" {
		t.Fatalf("empty table: parseDefaultRoute = %q, want empty", got)
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return port
}
