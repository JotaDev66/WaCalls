package main

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"wacalls/internal/voip/media"

	"github.com/pion/webrtc/v4"
)

// makeBrowserVideoOffer simulates a video-call browser: it opens both the "pcm"
// and "vp8" data channels and returns the offer plus the vp8 channel.
func makeBrowserVideoOffer(t *testing.T) (*webrtc.PeerConnection, *webrtc.DataChannel, string) {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.CreateDataChannel(pcmChannelLabel, nil); err != nil {
		t.Fatal(err)
	}
	vdc, err := pc.CreateDataChannel(videoChannelLabel, nil)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gather
	return pc, vdc, pc.LocalDescription().SDP
}

func TestBridgeVideoRoundtrip(t *testing.T) {
	pc, vdc, offer := makeBrowserVideoOffer(t)
	defer pc.Close()

	fromBrowser := make(chan media.VideoFrame, 1)
	br, answer, err := NewBridge(offer, slog.Default())
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	defer br.Close()
	br.OnBrowserVideo = func(f media.VideoFrame) { fromBrowser <- f }

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatalf("browser SetRemoteDescription: %v", err)
	}

	want := media.VideoFrame{Keyframe: true, TimestampMS: 90, Data: []byte{9, 8, 7, 6, 5}}

	toBrowser := make(chan media.VideoFrame, 1)
	vdc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if f, ok := media.DecodeVideoFrame(msg.Data); ok {
			toBrowser <- f
		}
	})
	vdc.OnOpen(func() {
		_ = vdc.Send(media.EncodeVideoFrame(want))
	})

	select {
	case got := <-fromBrowser:
		if got.Keyframe != want.Keyframe || got.TimestampMS != want.TimestampMS || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("browser->bridge frame mismatch: %+v vs %+v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for OnBrowserVideo")
	}

	// bridge -> browser
	if err := br.WriteVideo(want); err != nil {
		t.Fatalf("WriteVideo: %v", err)
	}
	select {
	case got := <-toBrowser:
		if got.Keyframe != want.Keyframe || got.TimestampMS != want.TimestampMS || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("bridge->browser frame mismatch: %+v vs %+v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the browser to receive the frame")
	}
}

func TestNewBridgeStillWorksWithoutVideoChannel(t *testing.T) {
	pc, _, offer := makeBrowserOffer(t)
	defer pc.Close()

	br, answer, err := NewBridge(offer, slog.Default())
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	defer br.Close()
	if answer == "" {
		t.Fatal("empty answer")
	}
	// WriteVideo before any vp8 channel exists must be a no-op, not a panic.
	if err := br.WriteVideo(media.VideoFrame{Data: []byte{1}}); err != nil {
		t.Fatalf("WriteVideo no-op returned error: %v", err)
	}
}
