package call

import (
	"context"
	"log/slog"
	"testing"

	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/signaling"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func offerNodeWithVoipSettings(callID string, from types.JID, blob string) *waBinary.Node {
	return &waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"from": from},
		Content: []waBinary.Node{{
			Tag:   "offer",
			Attrs: waBinary.Attrs{"call-id": callID, "call-creator": from.String()},
			Content: []waBinary.Node{{
				Tag:     "voip_settings",
				Attrs:   waBinary.Attrs{"uncompressed": "1"},
				Content: []byte(blob),
			}},
		}},
	}
}

func TestHandleOfferSelectsOpusFromVoipSettings(t *testing.T) {
	sock := newCtxQuerySock()
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0, func(string, *CallManager) {}, nil)
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(),
		offerNodeWithVoipSettings("CALL1", peer, `{"encode":{"use_mlow_codec_v1":"false"}}`), peer)

	cm, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("offer must register a call manager")
	}
	if got := cm.CurrentCall().Codec; got != signaling.CodecOpus {
		t.Fatalf("want codec opus from voip_settings, got %q", got)
	}
}

func TestHandleOfferWithoutVoipSettingsKeepsMlow(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)
	if got := cm.CurrentCall().Codec; got != signaling.CodecMLow {
		t.Fatalf("want default codec mlow, got %q", got)
	}
}

func TestApplyVoipSettingsFindsNestedBlob(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	// The outbound ack nests voip_settings under the echoed offer child.
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	ack := &waBinary.Node{
		Tag:   "ack",
		Attrs: waBinary.Attrs{"type": "offer"},
		Content: []waBinary.Node{
			*offerNodeWithVoipSettings("CALL1", peer, `{"encode":{"use_mlow_codec_v1":"false"}}`),
		},
	}
	cm.applyVoipSettings(ack, "CALL1")
	if got := cm.CurrentCall().Codec; got != signaling.CodecOpus {
		t.Fatalf("want codec opus from nested voip_settings, got %q", got)
	}
}

func TestApplyVoipSettingsIgnoresOtherCall(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	cm.applyVoipSettings(
		offerNodeWithVoipSettings("OTHER", peer, `{"encode":{"use_mlow_codec_v1":"false"}}`), "OTHER")
	if got := cm.CurrentCall().Codec; got != signaling.CodecMLow {
		t.Fatalf("settings for another call must not change the codec, got %q", got)
	}
}

func TestApplyVoipSettingsMalformedKeepsMlow(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	cm.applyVoipSettings(offerNodeWithVoipSettings("CALL1", peer, "{not json"), "CALL1")
	if got := cm.CurrentCall().Codec; got != signaling.CodecMLow {
		t.Fatalf("malformed settings must keep mlow, got %q", got)
	}
}
