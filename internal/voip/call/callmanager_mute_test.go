package call

import (
	"context"
	"errors"
	"testing"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/signaling"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func muteNode(callID, muteState string, from types.JID) *waBinary.Node {
	return &waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"from": from},
		Content: []waBinary.Node{{
			Tag: "mute_v2",
			Attrs: waBinary.Attrs{
				"call-id": callID, "call-creator": from.String(), "mute-state": muteState,
			},
		}},
	}
}

type peerMuteEvent struct {
	callID string
	muted  bool
}

func TestHandleCallMuteFiresOnPeerMute(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	var got []peerMuteEvent
	cm.OnPeerMute = func(callID string, muted bool) {
		got = append(got, peerMuteEvent{callID, muted})
	}

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	cm.HandleCallMute(muteNode("CALL1", "1", peer))
	cm.HandleCallMute(muteNode("CALL1", "0", peer))

	want := []peerMuteEvent{{"CALL1", true}, {"CALL1", false}}
	if len(got) != len(want) {
		t.Fatalf("want %d events, got %+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: want %+v, got %+v", i, want[i], got[i])
		}
	}
}

func TestHandleCallMuteNilCallbackSafe(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	cm.HandleCallMute(muteNode("CALL1", "1", peer))
}

func TestHandleCallMuteIgnoresEndedCall(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	if err := cm.EndCall(context.Background(), core.EndCallReasonUserEnded); err != nil {
		t.Fatalf("end call: %v", err)
	}

	fired := false
	cm.OnPeerMute = func(string, bool) { fired = true }
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	cm.HandleCallMute(muteNode("CALL1", "1", peer))
	if fired {
		t.Fatal("mute on an ended call must not fire OnPeerMute")
	}
}

func activeManager(t *testing.T, sock signaling.Socket) *CallManager {
	t.Helper()
	cm := ringingManager(t, sock)
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.currentCall.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := cm.currentCall.ApplyTransition(Transition{Type: TransitionMediaConnected}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return cm
}

func TestSetMuteSendsMuteV2ToAnsweringDevice(t *testing.T) {
	sock := newCtxQuerySock()
	cm := activeManager(t, sock)

	answering := "62440234549366:33@lid"
	cm.mu.Lock()
	cm.acceptedByJid = answering
	cm.mu.Unlock()

	if err := cm.SetMute(context.Background(), true); err != nil {
		t.Fatalf("set mute: %v", err)
	}
	awaitQuery(t, sock)
	q := sock.queried()
	if len(q) != 1 || q[0].tag != "mute_v2" {
		t.Fatalf("want exactly one mute_v2 stanza, got %+v", q)
	}
	if q[0].to != answering {
		t.Fatalf("mute_v2 must target the answering device %q, got to=%q", answering, q[0].to)
	}
	if state := q[0].childAttrs["mute-state"]; state != "1" {
		t.Fatalf("want mute-state 1, got %v", state)
	}
	if !cm.CurrentCall().StateData.AudioMuted {
		t.Fatal("AudioMuted must be true after SetMute(true)")
	}
}

func TestSetMuteUnmuteSendsZero(t *testing.T) {
	sock := newCtxQuerySock()
	cm := activeManager(t, sock)

	if err := cm.SetMute(context.Background(), true); err != nil {
		t.Fatalf("mute: %v", err)
	}
	awaitQuery(t, sock)
	if err := cm.SetMute(context.Background(), false); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	awaitQuery(t, sock)

	q := sock.queried()
	if len(q) != 2 || q[1].tag != "mute_v2" {
		t.Fatalf("want two mute_v2 stanzas, got %+v", q)
	}
	if state := q[1].childAttrs["mute-state"]; state != "0" {
		t.Fatalf("want mute-state 0 on unmute, got %v", state)
	}
	if cm.CurrentCall().StateData.AudioMuted {
		t.Fatal("AudioMuted must be false after SetMute(false)")
	}
}

func TestSetMuteBeforeActiveIsInvalid(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	err := cm.SetMute(context.Background(), true)
	var invalid *InvalidTransition
	if !errors.As(err, &invalid) {
		t.Fatalf("mute of a ringing call must return InvalidTransition, got %v", err)
	}
	for _, q := range sock.queried() {
		if q.tag == "mute_v2" {
			t.Fatal("invalid mute must not reach the wire")
		}
	}
}
