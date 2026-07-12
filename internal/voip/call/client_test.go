package call

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type recordSock struct {
	fakeSock
	mu   sync.Mutex
	sent []waBinary.Node
}

func (r *recordSock) SendNode(ctx context.Context, node waBinary.Node) error {
	r.mu.Lock()
	r.sent = append(r.sent, node)
	r.mu.Unlock()
	return nil
}

func (r *recordSock) sentInnerTags() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var tags []string
	for i := range r.sent {
		for _, child := range wanode.NodeChildren(&r.sent[i]) {
			tags = append(tags, child.Tag)
		}
	}
	return tags
}

func offerNode(callID string, from types.JID) *waBinary.Node {
	return &waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"from": from},
		Content: []waBinary.Node{{
			Tag:   "offer",
			Attrs: waBinary.Attrs{"call-id": callID, "call-creator": from.String()},
		}},
	}
}

func TestHandleOfferIdempotent(t *testing.T) {
	sock := &recordSock{}
	onCallCount := 0
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) { onCallCount++ }, nil)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	defer func() { _ = c.EndCall(context.Background(), "CALL1", core.EndCallReasonUserEnded) }()

	first, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("first offer must register a call manager")
	}

	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)

	if n := c.Count(); n != 1 {
		t.Fatalf("retransmitted offer must not create a second call, got %d", n)
	}
	second, _ := c.Get("CALL1")
	if second != first {
		t.Fatal("retransmitted offer must keep the original call manager")
	}
	if onCallCount != 1 {
		t.Fatalf("onCall must fire once, got %d", onCallCount)
	}
}

func TestHandleOfferDuplicateNotRejectedAtCapacity(t *testing.T) {
	sock := &recordSock{}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 1,
		func(string, *CallManager) {}, nil)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	defer func() { _ = c.EndCall(context.Background(), "CALL1", core.EndCallReasonUserEnded) }()
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)

	for _, tag := range sock.sentInnerTags() {
		if tag == "reject" {
			t.Fatal("retransmitted offer of a live call must not be rejected at capacity")
		}
	}
	if n := c.Count(); n != 1 {
		t.Fatalf("expected 1 live call, got %d", n)
	}
}
