package transport

import "testing"

func TestIsRtcpPacket(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want bool
	}{
		{"sr", []byte{0x80, 0xc8}, true},
		{"compact", []byte{0x81, 0xd0}, true},
		{"rtp", []byte{0x90, 0x78}, false},
		{"stun", []byte{0x00, 0x01}, false},
		{"short", []byte{0x80}, false},
	}
	for _, c := range cases {
		if got := IsRtcpPacket(c.b); got != c.want {
			t.Errorf("%s: IsRtcpPacket(%x)=%v want %v", c.name, c.b, got, c.want)
		}
	}
}
