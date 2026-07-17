package doctor

import (
	"errors"
	"net"
	"testing"
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
