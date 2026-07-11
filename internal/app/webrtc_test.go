package app

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestPublicIPs(t *testing.T) {
	t.Setenv("WACALLS_PUBLIC_IP", "  203.0.113.10 , , 198.51.100.7 ")
	got := publicIPs()
	want := []string{"203.0.113.10", "198.51.100.7"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	t.Setenv("WACALLS_PUBLIC_IP", "")
	if ips := publicIPs(); ips != nil {
		t.Fatalf("expected nil for empty env, got %v", ips)
	}
}

func TestBuildBrowserAPIDefault(t *testing.T) {
	api, err := buildBrowserAPI(0, nil)
	if err != nil || api == nil {
		t.Fatalf("default api: got (%v, %v)", api, err)
	}
}

// TestBuildBrowserAPIMux proves the core Docker requirement: with a fixed UDP
// port and a public IP, the gathered SDP advertises a host candidate carrying
// that exact IP and port — i.e. what the browser will dial through the 1:1 NAT.
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
