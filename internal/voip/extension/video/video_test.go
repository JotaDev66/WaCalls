package video

import (
	"bytes"
	"testing"

	"wacalls/internal/voip/codec/h264"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/media"
)

type fakeRelay struct{}

func (f *fakeRelay) Broadcast(data []byte)                        {}
func (f *fakeRelay) BufferedAmount() uint64                       { return 0 }
func (f *fakeRelay) HasConnection() bool                          { return true }
func (f *fakeRelay) SetStreamSsrcs(selfSsrcs, peerSsrcs []uint32) {}

func TestVideoFeedAUSends(t *testing.T) {
	sent := 0
	scope := &engine.CallScope{
		CallID:          "c1",
		OwnDeviceJID:    "a.0",
		PeerDeviceJID:   "b.0",
		Relay:           &fakeRelay{},
		SendRTP:         func(*media.RtpPacket) error { sent++; return nil },
		OnRTP:           func(uint8, func(*media.RtpPacket)) {},
		DeclareSelfSSRC: func(uint32) {},
	}
	v := New()
	if err := v.Attach(scope); err != nil {
		t.Fatalf("attach: %v", err)
	}
	v.FeedAU([]byte{0, 0, 0, 1, 0x67, 0xAA, 0xBB, 0xCC})
	if sent == 0 {
		t.Fatal("no SendRTP")
	}
}

func TestVideoEmitsFrameFromInbound(t *testing.T) {
	scope := &engine.CallScope{
		CallID:          "c1",
		OwnDeviceJID:    "a.0",
		PeerDeviceJID:   "b.0",
		Relay:           &fakeRelay{},
		SendRTP:         func(*media.RtpPacket) error { return nil },
		OnRTP:           func(uint8, func(*media.RtpPacket)) {},
		DeclareSelfSSRC: func(uint32) {},
	}
	v := New()
	if err := v.Attach(scope); err != nil {
		t.Fatalf("attach: %v", err)
	}

	payloads := h264.PackageH264NALU([]byte{0x67, 0xAA, 0xBB, 0xCC})
	pkt := &media.RtpPacket{
		Header:  media.NewRtpHeader(core.PayloadTypeWhatsAppH264, 1, 0, 9999),
		Payload: payloads[0],
	}
	pkt.Header.Marker = true

	var got []byte
	v.OnPeerAU(func(f []byte) { got = f })
	v.handleInbound(pkt)

	if got == nil || !bytes.Contains(got, []byte{0x67, 0xAA, 0xBB, 0xCC}) {
		t.Fatalf("expected frame containing nalu, got %v", got)
	}
}
