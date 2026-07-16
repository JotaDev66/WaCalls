package signaling

import (
	"bytes"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func structuredRelayNode() waBinary.Node {
	return waBinary.Node{Tag: "relay", Attrs: waBinary.Attrs{"uuid": "u1"}, Content: []waBinary.Node{
		{Tag: "key", Content: []byte("stunkey")},
		{Tag: "token", Attrs: waBinary.Attrs{"id": "1"}, Content: []byte{0xAA, 0xBB}},
		{Tag: "auth_token", Attrs: waBinary.Attrs{"id": "2"}, Content: []byte{0xCC}},
		{Tag: "te2", Attrs: waBinary.Attrs{"relay_id": "7", "relay_name": "gru1", "token_id": "1", "auth_token_id": "2", "protocol": "0"}, Content: []byte{170, 78, 21, 99, 0x0D, 0x98}},
	}}
}

func TestFindRelayNodeRecursive(t *testing.T) {
	relay := structuredRelayNode()
	inner := waBinary.Node{Tag: "transport", Content: []waBinary.Node{
		{Tag: "net", Attrs: waBinary.Attrs{"medium": "2"}},
		{Tag: "wrapper", Content: []waBinary.Node{relay}},
	}}
	found := FindRelayNode(&inner)
	if found == nil || found.Tag != "relay" {
		t.Fatal("FindRelayNode must locate a nested relay block")
	}
}

func TestParseRelayFromNodeStructuredTransport(t *testing.T) {
	relay := structuredRelayNode()
	inner := waBinary.Node{Tag: "transport", Content: []waBinary.Node{relay}}
	parsed := ParseRelayFromNode(&inner)
	if len(parsed.Relays) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(parsed.Relays))
	}
	ep := parsed.Relays[0]
	if ep.IP != "170.78.21.99" || ep.Port != 3480 {
		t.Fatalf("bad addr %s:%d", ep.IP, ep.Port)
	}
	if !bytes.Equal(ep.RawToken, []byte{0xAA, 0xBB}) || ep.Key != "stunkey" {
		t.Fatal("token/key must come from the structured block")
	}
	if ep.AuthTokenID != "2" || ep.RelayName != "gru1" {
		t.Fatalf("bad auth/name %s %s", ep.AuthTokenID, ep.RelayName)
	}
	if parsed.UUID != "u1" {
		t.Fatalf("bad uuid %s", parsed.UUID)
	}
}

func TestParseRelayFromNodeWithoutRelay(t *testing.T) {
	inner := waBinary.Node{Tag: "transport", Content: []waBinary.Node{{Tag: "net"}}}
	if parsed := ParseRelayFromNode(&inner); len(parsed.Relays) != 0 {
		t.Fatal("no relay block must parse to zero endpoints")
	}
}

func TestDecodeLatency(t *testing.T) {
	if got := DecodeLatency("33554477"); got != 45 {
		t.Fatalf("33554477 must decode to 45, got %d", got)
	}
	for _, bad := range []string{"100", "x", ""} {
		if got := DecodeLatency(bad); got != 0 {
			t.Fatalf("%q must decode to 0, got %d", bad, got)
		}
	}
}
