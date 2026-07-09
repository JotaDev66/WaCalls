package app

import (
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func TestCallIDFromNode(t *testing.T) {
	node := &waBinary.Node{
		Tag: "call",
		Content: []waBinary.Node{{
			Tag:   "offer",
			Attrs: waBinary.Attrs{"call-id": "ABC123"},
		}},
	}
	if got := callIDFromNode(node); got != "ABC123" {
		t.Fatalf("expected ABC123, got %q", got)
	}

	empty := &waBinary.Node{Tag: "call"}
	if got := callIDFromNode(empty); got != "" {
		t.Fatalf("node with no children must yield empty, got %q", got)
	}
}
