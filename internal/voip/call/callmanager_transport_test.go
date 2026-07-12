package call

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/transport"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func transportCallNode(children ...waBinary.Node) *waBinary.Node {
	inner := waBinary.Node{Tag: "transport", Attrs: waBinary.Attrs{"call-id": "c1", "transport-message-type": "1"}, Content: children}
	return &waBinary.Node{Tag: "call", Attrs: waBinary.Attrs{"from": types.NewJID("peer", "lid")}, Content: []waBinary.Node{inner}}
}

func structuredRelayChild() waBinary.Node {
	return waBinary.Node{Tag: "relay", Attrs: waBinary.Attrs{"uuid": "u1"}, Content: []waBinary.Node{
		{Tag: "key", Content: []byte("stunkey")},
		{Tag: "token", Attrs: waBinary.Attrs{"id": "1"}, Content: []byte{0xAA}},
		{Tag: "te2", Attrs: waBinary.Attrs{"relay_name": "gru1", "token_id": "1", "protocol": "0"}, Content: []byte{9, 9, 9, 9, 0x0D, 0x98}},
	}}
}

func TestTransportStructuredRelayDialsDuringOutage(t *testing.T) {
	configured := make(chan int, 1)
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{noConn: true, onConfigure: func(r []transport.RelayConfig) { configured <- len(r) }}
	m.currentCall = activeCall()

	m.HandleCallTransport(context.Background(), transportCallNode(structuredRelayChild()), types.NewJID("peer", "lid"))

	if m.currentCall.RelayData == nil || len(m.currentCall.RelayData.Endpoints) != 1 {
		t.Fatal("structured transport relay must populate RelayData")
	}
	if m.currentCall.RelayData.Endpoints[0].RawToken == nil {
		t.Fatal("te2 endpoint must carry RawToken")
	}
	select {
	case n := <-configured:
		if n != 1 {
			t.Fatalf("expected 1 dialable config, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dial was not attempted")
	}
}

func TestTransportUndialableKeepsStoredEndpoints(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{noConn: true}
	m.currentCall = activeCall()
	stored := []core.RelayEndpoint{{IP: "1.1.1.1", Port: 3480, Key: "k", RawToken: []byte{1}}}
	m.currentCall.RelayData = &core.RelayData{Endpoints: stored}

	attrRelay := waBinary.Node{Tag: "relay", Attrs: waBinary.Attrs{"ip": "2.2.2.2", "token": "tk"}}
	m.HandleCallTransport(context.Background(), transportCallNode(attrRelay), types.NewJID("peer", "lid"))

	if got := m.currentCall.RelayData.Endpoints[0].IP; got != "1.1.1.1" {
		t.Fatalf("undialable transport list must not clobber stored endpoints, got %s", got)
	}
}

func latencyCallNode(callID string, children ...waBinary.Node) *waBinary.Node {
	inner := waBinary.Node{Tag: "relaylatency", Attrs: waBinary.Attrs{"call-id": callID, "call-creator": "creator@lid"}, Content: children}
	return &waBinary.Node{Tag: "call", Attrs: waBinary.Attrs{"from": types.NewJID("peer", "lid")}, Content: []waBinary.Node{inner}}
}

func teProbe(latency, name string, addr []byte) waBinary.Node {
	return waBinary.Node{Tag: "te", Attrs: waBinary.Attrs{"latency": latency, "relay_name": name}, Content: addr}
}

func sentRelayLatencies(sock *recordSock) []waBinary.Node {
	sock.mu.Lock()
	defer sock.mu.Unlock()
	var out []waBinary.Node
	for i := range sock.sent {
		for _, child := range wanode.NodeChildren(&sock.sent[i]) {
			if child.Tag == "relaylatency" {
				out = append(out, child)
			}
		}
	}
	return out
}

func TestRelayLatencyEchoesProbesVerbatim(t *testing.T) {
	sock := &recordSock{}
	m := NewCallManager(sock, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)

	m.HandleCallRelayLatency(context.Background(),
		latencyCallNode("c1", teProbe("33554477", "gru1", []byte{1, 2, 3, 4, 5, 6}), teProbe("33554533", "for2", []byte{9, 9, 9, 9, 0, 80})),
		wanode.MustJID("peer@lid"))

	echoes := sentRelayLatencies(sock)
	if len(echoes) != 2 {
		t.Fatalf("expected 2 echo stanzas, got %d", len(echoes))
	}
	tes := wanode.NodeChildren(&echoes[0])
	if len(tes) != 1 || wanode.AttrString(tes[0].Attrs, "latency") != "33554477" || wanode.AttrString(tes[0].Attrs, "relay_name") != "gru1" {
		t.Fatalf("first echo must repeat the probe verbatim: %+v", tes)
	}
	if !bytes.Equal(wanode.NodeBytes(&tes[0]), []byte{1, 2, 3, 4, 5, 6}) {
		t.Fatal("echo must carry the probe address bytes verbatim")
	}
}

func TestRelayLatencyNoEchoForOutgoing(t *testing.T) {
	sock := &recordSock{}
	m := NewCallManager(sock, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = NewOutgoingCall("c1", "peer@lid", "me@lid", core.CallMediaTypeAudio)

	m.HandleCallRelayLatency(context.Background(), latencyCallNode("c1", teProbe("33554477", "gru1", []byte{1, 2, 3, 4, 5, 6})), wanode.MustJID("peer@lid"))

	if n := len(sentRelayLatencies(sock)); n != 0 {
		t.Fatalf("outgoing call must not echo probes, sent %d", n)
	}
}

func TestRelayLatencyHarvestsRelayWhenAbsent(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = NewIncomingCall("c1", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)

	m.HandleCallRelayLatency(context.Background(), latencyCallNode("c1", structuredRelayChild()), wanode.MustJID("peer@lid"))

	if m.currentCall.RelayData == nil || len(m.currentCall.RelayData.Endpoints) != 1 {
		t.Fatal("relaylatency must harvest an embedded relay block when RelayData is absent")
	}
}

func TestClientRoutesRelayLatencyByCallID(t *testing.T) {
	sock := &recordSock{}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0, func(string, *CallManager) {}, nil)
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	defer func() { _ = c.EndCall(context.Background(), "CALL1", core.EndCallReasonUserEnded) }()

	before := len(sentRelayLatencies(sock))
	c.HandleRelayLatency(context.Background(), latencyCallNode("CALL1", teProbe("33554477", "gru1", []byte{1, 2, 3, 4, 5, 6})), peer)
	if got := len(sentRelayLatencies(sock)) - before; got != 1 {
		t.Fatalf("known call-id must echo 1 probe, got %d", got)
	}

	before = len(sentRelayLatencies(sock))
	c.HandleRelayLatency(context.Background(), latencyCallNode("NOPE", teProbe("33554477", "gru1", []byte{1, 2, 3, 4, 5, 6})), peer)
	if got := len(sentRelayLatencies(sock)) - before; got != 0 {
		t.Fatalf("unknown call-id must be a no-op, got %d echoes", got)
	}
}

func TestTransportIgnoredWhenConnected(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = activeCall()

	m.HandleCallTransport(context.Background(), transportCallNode(structuredRelayChild()), types.NewJID("peer", "lid"))

	if m.currentCall.RelayData != nil {
		t.Fatal("transport must stay ignored while a relay connection is open")
	}
}
