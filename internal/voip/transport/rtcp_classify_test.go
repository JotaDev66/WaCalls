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
		{"rr with two blocks", []byte{0x82, 0xc9}, true},
		{"profile bit", []byte{0x91, 0xce}, true},
		{"rtp", []byte{0x90, 0x78}, false},
		{"rtp marker pt120", []byte{0x80, 0xf8}, false},
		{"rtp high pt", []byte{0x90, 0xe1}, false},
		{"stun", []byte{0x00, 0xc8}, false},
		{"short", []byte{0x80}, false},
	}
	for _, c := range cases {
		if got := IsRtcpPacket(c.b); got != c.want {
			t.Errorf("%s: IsRtcpPacket(%x)=%v want %v", c.name, c.b, got, c.want)
		}
	}
}

func TestIsRtpPacket(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want bool
	}{
		{"opus with extension", []byte{0x90, 0x78}, true},
		{"marker pt120", []byte{0x80, 0xf8}, true},
		{"high pt", []byte{0x90, 0xe1}, true},
		{"rtcp sr", []byte{0x80, 0xc8}, false},
		{"rtcp rr two blocks", []byte{0x82, 0xc9}, false},
		{"version 3 junk", []byte{0xff, 0xff}, false},
		{"stun", []byte{0x00, 0x01}, false},
		{"short", []byte{0x90}, false},
	}
	for _, c := range cases {
		if got := IsRtpPacket(c.b); got != c.want {
			t.Errorf("%s: IsRtpPacket(%x)=%v want %v", c.name, c.b, got, c.want)
		}
	}
}
