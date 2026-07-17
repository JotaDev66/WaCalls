package call

import (
	"context"
	"log/slog"
	"testing"

	"wacalls/internal/voip/signaling"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type sentStanza struct {
	tag    string
	to     string
	reason string
	dests  []string
}

type elsewhereSock struct {
	recordSock
	devices []types.JID
}

func (s *elsewhereSock) GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error) {
	return s.devices, nil
}

func (s *elsewhereSock) sentStanzas() []sentStanza {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []sentStanza
	for i := range s.sent {
		node := &s.sent[i]
		children := wanode.NodeChildren(node)
		if len(children) == 0 {
			continue
		}
		inner := children[0]
		st := sentStanza{
			tag:    inner.Tag,
			to:     wanode.AttrString(node.Attrs, "to"),
			reason: wanode.AttrString(inner.Attrs, "reason"),
		}
		if dst := wanode.FindChildByTag(&inner, "destination"); dst != nil {
			for _, to := range wanode.NodeChildren(dst) {
				st.dests = append(st.dests, wanode.AttrString(to.Attrs, "jid"))
			}
		}
		out = append(out, st)
	}
	return out
}

func acceptNode(callID string, from types.JID) *waBinary.Node {
	return &waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"from": from},
		Content: []waBinary.Node{{
			Tag:   "accept",
			Attrs: waBinary.Attrs{"call-id": callID, "call-creator": "caller@lid"},
		}},
	}
}

func terminateNode(callID string, from types.JID) *waBinary.Node {
	return &waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"from": from},
		Content: []waBinary.Node{{
			Tag:   "terminate",
			Attrs: waBinary.Attrs{"call-id": callID, "call-creator": "caller@lid"},
		}},
	}
}

func outgoingRingingManager(t *testing.T, sock signaling.Socket) *CallManager {
	t.Helper()
	cm := NewCallManager(sock, slog.Default())
	cm.relay = &fakeRelay{noConn: true}
	peer := types.NewJID("62440234549366", types.HiddenUserServer)
	if err := cm.StartCall(context.Background(), "CALL1", peer); err != nil {
		t.Fatalf("start call: %v", err)
	}
	t.Cleanup(cm.cleanupMedia)
	return cm
}

func lidDevice(user string, device uint16) types.JID {
	j := types.NewJID(user, types.HiddenUserServer)
	j.Device = device
	return j
}

func TestStartCallPersistsCalleeDevices(t *testing.T) {
	devs := []types.JID{lidDevice("62440234549366", 0), lidDevice("62440234549366", 33)}
	sock := &elsewhereSock{devices: devs}
	cm := outgoingRingingManager(t, sock)

	cm.mu.Lock()
	got := len(cm.calleeDevices)
	cm.mu.Unlock()
	if got != 2 {
		t.Fatalf("calleeDevices = %d, want 2", got)
	}
}

func TestCompanionAcceptStopsSiblingDevices(t *testing.T) {
	primary := lidDevice("62440234549366", 0)
	companion := lidDevice("62440234549366", 33)
	sock := &elsewhereSock{devices: []types.JID{primary, companion}}
	cm := outgoingRingingManager(t, sock)

	cm.HandleCallAccept(context.Background(), acceptNode("CALL1", companion), companion)

	var elsewhere *sentStanza
	for _, st := range sock.sentStanzas() {
		if st.tag == "terminate" && st.reason == "accepted_elsewhere" {
			s := st
			elsewhere = &s
		}
	}
	if elsewhere == nil {
		t.Fatalf("no accepted_elsewhere terminate sent; stanzas: %+v", sock.sentStanzas())
	}
	if len(elsewhere.dests) != 1 || elsewhere.dests[0] != primary.String() {
		t.Fatalf("destination must list only the non-answering primary %s, got %v", primary, elsewhere.dests)
	}
}

func TestAcceptWithoutSiblingsSendsNoElsewhere(t *testing.T) {
	only := lidDevice("62440234549366", 0)
	sock := &elsewhereSock{devices: []types.JID{only}}
	cm := outgoingRingingManager(t, sock)

	cm.HandleCallAccept(context.Background(), acceptNode("CALL1", only), only)

	for _, st := range sock.sentStanzas() {
		if st.reason == "accepted_elsewhere" {
			t.Fatalf("single-device callee must not receive an elsewhere terminate: %+v", st)
		}
	}
}

func TestSecondAcceptFromAnotherDeviceIsIgnored(t *testing.T) {
	primary := lidDevice("62440234549366", 0)
	companion := lidDevice("62440234549366", 33)
	sock := &elsewhereSock{devices: []types.JID{primary, companion}}
	cm := outgoingRingingManager(t, sock)

	cm.HandleCallAccept(context.Background(), acceptNode("CALL1", companion), companion)
	before := len(sock.sentStanzas())
	cm.HandleCallAccept(context.Background(), acceptNode("CALL1", primary), primary)

	cm.mu.Lock()
	accepted := cm.acceptedByJid
	cm.mu.Unlock()
	if accepted != companion.String() {
		t.Fatalf("first accept must win: acceptedByJid = %q, want %q", accepted, companion)
	}
	if after := len(sock.sentStanzas()); after != before {
		t.Fatalf("duplicate accept must not send stanzas: %d -> %d", before, after)
	}
}

func TestLateRejectFromNonAnsweringDeviceKeepsCall(t *testing.T) {
	primary := lidDevice("62440234549366", 0)
	companion := lidDevice("62440234549366", 33)
	sock := &elsewhereSock{devices: []types.JID{primary, companion}}
	cm := outgoingRingingManager(t, sock)

	cm.HandleCallAccept(context.Background(), acceptNode("CALL1", companion), companion)
	cm.HandleCallTerminate(terminateNode("CALL1", primary))

	if cm.CurrentCall().IsEnded() {
		t.Fatal("terminate from the non-answering device must not end an accepted call")
	}

	cm.HandleCallTerminate(terminateNode("CALL1", companion))
	if !cm.CurrentCall().IsEnded() {
		t.Fatal("terminate from the answering device must end the call")
	}
}
