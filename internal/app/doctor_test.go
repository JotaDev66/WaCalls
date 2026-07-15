package app

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckUDPPort(t *testing.T) {
	if got := checkUDPPort(0); got.status != statusInfo {
		t.Fatalf("port 0: want info, got %v", got.status)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	busy := conn.LocalAddr().(*net.UDPAddr).Port
	if got := checkUDPPort(busy); got.status != statusFail {
		t.Fatalf("busy port %d: want fail, got %v (%s)", busy, got.status, got.detail)
	}

	free, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	freePort := free.LocalAddr().(*net.UDPAddr).Port
	_ = free.Close()
	if got := checkUDPPort(freePort); got.status != statusOK {
		t.Fatalf("free port %d: want ok, got %v (%s)", freePort, got.status, got.detail)
	}
}

func TestCheckPublicIP(t *testing.T) {
	cases := []struct {
		name       string
		configured []string
		local      []net.IP
		want       checkStatus
	}{
		{"public configured", []string{"1.2.3.4"}, nil, statusOK},
		{"private configured", []string{"192.168.1.1"}, nil, statusWarn},
		{"unparseable configured", []string{"bogus"}, nil, statusFail},
		{"unset behind nat", nil, []net.IP{net.ParseIP("192.168.1.5")}, statusWarn},
		{"unset with public local", nil, []net.IP{net.ParseIP("8.8.8.8")}, statusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := checkPublicIP(c.configured, c.local); got.status != c.want {
				t.Fatalf("want %v, got %v (%s)", c.want, got.status, got.detail)
			}
		})
	}
}

func TestCheckDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doctor.db")
	if got := checkDatabase(context.Background(), storeConfigFor(Config{DBPath: path})); got.status != statusOK {
		t.Fatalf("temp sqlite: want ok, got %v (%s)", got.status, got.detail)
	}
}

func TestDoctorReport(t *testing.T) {
	cfg := Config{
		DBPath:        filepath.Join(t.TempDir(), "doctor.db"),
		WebRTCUDPPort: 0,
		PublicIPs:     []string{"1.2.3.4"},
	}
	var b strings.Builder
	if ok := Doctor(context.Background(), cfg, &b); !ok {
		t.Fatalf("healthy config: want ok=true\n%s", b.String())
	}
	if out := b.String(); !strings.Contains(out, "database") || !strings.Contains(out, "relay") {
		t.Fatalf("report missing lines:\n%s", out)
	}
}
