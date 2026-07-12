package call

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/transport"

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

func TestTransportIgnoredWhenConnected(t *testing.T) {
	m := NewCallManager(fakeSock{}, slog.Default())
	m.relay = &fakeRelay{}
	m.currentCall = activeCall()

	m.HandleCallTransport(context.Background(), transportCallNode(structuredRelayChild()), types.NewJID("peer", "lid"))

	if m.currentCall.RelayData != nil {
		t.Fatal("transport must stay ignored while a relay connection is open")
	}
}
