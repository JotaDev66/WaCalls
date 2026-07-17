package doctor

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func TestCheckExternalIP(t *testing.T) {
	ip := net.ParseIP("203.0.113.9")
	boom := errors.New("boom")
	cases := []struct {
		name       string
		configured []string
		ip         net.IP
		err        error
		want       checkStatus
	}{
		{"confirmed", []string{"203.0.113.9"}, ip, nil, statusOK},
		{"mismatch", []string{"198.51.100.7"}, ip, nil, statusWarn},
		{"discovered unset", nil, ip, nil, statusInfo},
		{"probe failed configured", []string{"203.0.113.9"}, nil, boom, statusInfo},
		{"probe failed unset", nil, nil, boom, statusWarn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := checkExternalIP(c.configured, c.ip, "stun.test:3478", c.err); got.status != c.want {
				t.Fatalf("want %v, got %v (%s)", c.want, got.status, got.detail)
			}
		})
	}
}

func startFakeSTUN(t *testing.T, respond net.IP, port int) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req := new(stun.Message)
			req.Raw = append([]byte(nil), buf[:n]...)
			if err := req.Decode(); err != nil {
				continue
			}
			resp := stun.MustBuild(stun.NewTransactionIDSetter(req.TransactionID), stun.BindingSuccess,
				stun.XORMappedAddress{IP: respond, Port: port})
			_, _ = conn.WriteToUDP(resp.Raw, from)
		}
	}()
	return conn.LocalAddr().String()
}

func shortSTUNTimeout(t *testing.T) {
	t.Helper()
	prev := stunProbeTimeout
	stunProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stunProbeTimeout = prev })
}

func deadUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	_ = conn.Close()
	return addr
}

func TestProbeExternalIP(t *testing.T) {
	shortSTUNTimeout(t)
	want := net.ParseIP("203.0.113.9")
	srv := startFakeSTUN(t, want, 4242)
	ip, server, err := probeExternalIP(context.Background(), []string{srv}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ip.Equal(want) {
		t.Fatalf("ip = %s, want %s", ip, want)
	}
	if server != srv {
		t.Fatalf("server = %s, want %s", server, srv)
	}
}

func TestProbeExternalIPFallback(t *testing.T) {
	shortSTUNTimeout(t)
	dead := deadUDPAddr(t)
	want := net.ParseIP("203.0.113.9")
	srv := startFakeSTUN(t, want, 4242)
	ip, server, err := probeExternalIP(context.Background(), []string{dead, srv}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ip.Equal(want) || server != srv {
		t.Fatalf("ip=%s server=%s, want %s via %s", ip, server, want, srv)
	}
}

func TestProbeExternalIPAllUnreachable(t *testing.T) {
	shortSTUNTimeout(t)
	if _, _, err := probeExternalIP(context.Background(), []string{deadUDPAddr(t)}, 0); err == nil {
		t.Fatal("want error for unreachable servers")
	}
	if _, _, err := probeExternalIP(context.Background(), nil, 0); err == nil {
		t.Fatal("want error for empty server list")
	}
}
