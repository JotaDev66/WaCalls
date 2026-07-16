package call

import (
	"bytes"
	"context"
	"errors"
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

type keyedSock struct {
	recordSock
	key      []byte
	err      error
	decrypts int
}

func (k *keyedSock) DecryptCallKey(ctx context.Context, from types.JID, encChild *waBinary.Node) ([]byte, error) {
	k.mu.Lock()
	k.decrypts++
	k.mu.Unlock()
	return k.key, k.err
}

func (k *keyedSock) decryptCount() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.decrypts
}

func offerNodeWithEnc(callID string, from types.JID) *waBinary.Node {
	return &waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"from": from},
		Content: []waBinary.Node{{
			Tag:   "offer",
			Attrs: waBinary.Attrs{"call-id": callID, "call-creator": from.String()},
			Content: []waBinary.Node{{
				Tag:   "enc",
				Attrs: waBinary.Attrs{"v": "2", "type": "pkmsg"},
			}},
		}},
	}
}

func sentRejectCount(sock *keyedSock) int {
	n := 0
	for _, tag := range sock.sentInnerTags() {
		if tag == "reject" {
			n++
		}
	}
	return n
}

func TestHandleOfferRejectsUndecryptableKey(t *testing.T) {
	sock := &keyedSock{err: errors.New("no valid sessions")}
	onCallCount := 0
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) { onCallCount++ }, nil)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNodeWithEnc("CALL1", peer), peer)

	if _, ok := c.Get("CALL1"); ok {
		t.Fatal("undecryptable offer must not register a call manager")
	}
	if onCallCount != 0 {
		t.Fatalf("onCall must not fire for undecryptable offer, got %d", onCallCount)
	}
	if n := c.Count(); n != 0 {
		t.Fatalf("undecryptable offer must not occupy a slot, got %d", n)
	}
	if sentRejectCount(sock) != 1 {
		t.Fatalf("expected exactly one reject stanza, sent tags: %v", sock.sentInnerTags())
	}
}

func TestHandleOfferKeyReachesCall(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	sock := &keyedSock{key: key}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) {}, nil)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNodeWithEnc("CALL1", peer), peer)
	defer func() { _ = c.EndCall(context.Background(), "CALL1", core.EndCallReasonUserEnded) }()

	cm, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("decryptable offer must register a call manager")
	}
	if got := cm.CurrentCall().EncryptionKey; !bytes.Equal(got, key) {
		t.Fatalf("EncryptionKey = %x, want %x", got, key)
	}
}

func TestHandleOfferNoEncNodeStillRings(t *testing.T) {
	sock := &keyedSock{err: errors.New("must not be called")}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) {}, nil)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	defer func() { _ = c.EndCall(context.Background(), "CALL1", core.EndCallReasonUserEnded) }()

	cm, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("offer without enc node must still register")
	}
	if cm.CurrentCall().EncryptionKey != nil {
		t.Fatal("EncryptionKey must stay nil without enc node")
	}
	if n := sock.decryptCount(); n != 0 {
		t.Fatalf("DecryptCallKey must not run without enc node, got %d", n)
	}
}

func TestCapacityRejectDoesNotDecrypt(t *testing.T) {
	sock := &keyedSock{key: bytes.Repeat([]byte{0x01}, 32)}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 1,
		func(string, *CallManager) {}, nil)

	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNodeWithEnc("CALL1", peer), peer)
	defer func() { _ = c.EndCall(context.Background(), "CALL1", core.EndCallReasonUserEnded) }()
	before := sock.decryptCount()
	c.HandleOffer(context.Background(), offerNodeWithEnc("CALL2", peer), peer)

	if got := sock.decryptCount(); got != before {
		t.Fatalf("capacity reject must not decrypt, count %d -> %d", before, got)
	}
	if sentRejectCount(sock) != 1 {
		t.Fatalf("expected capacity reject stanza, tags: %v", sock.sentInnerTags())
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

func TestClientRoutesByCallID(t *testing.T) {
	sock := &recordSock{}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) {}, nil)
	peerA := types.NewJID("5511999990001", types.DefaultUserServer)
	peerB := types.NewJID("5511999990002", types.DefaultUserServer)
	ctx := context.Background()

	c.HandleOffer(ctx, offerNode("CALL1", peerA), peerA)
	c.HandleOffer(ctx, offerNode("CALL2", peerB), peerB)
	defer func() { _ = c.EndCall(ctx, "CALL1", core.EndCallReasonUserEnded) }()
	defer func() { _ = c.EndCall(ctx, "CALL2", core.EndCallReasonUserEnded) }()

	if n := c.Count(); n != 2 {
		t.Fatalf("two distinct offers must register two calls, got %d", n)
	}
	cm1, ok1 := c.Get("CALL1")
	cm2, ok2 := c.Get("CALL2")
	if !ok1 || !ok2 || cm1 == nil || cm2 == nil || cm1 == cm2 {
		t.Fatalf("each callID must map to a distinct call manager (ok1=%v ok2=%v same=%v)", ok1, ok2, cm1 == cm2)
	}

	c.HandleTerminate(offerNode("CALL1", peerA))
	if s, _ := stateOf(cm1); s != core.CallStateEnded {
		t.Fatalf("terminate of CALL1 must end cm1, state=%s", s)
	}
	if s, _ := stateOf(cm2); s == core.CallStateEnded {
		t.Fatal("terminate of CALL1 must not touch cm2")
	}
}

func TestClientRejectsNewCallAtCapacity(t *testing.T) {
	sock := &recordSock{}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 1,
		func(string, *CallManager) {}, nil)
	peerA := types.NewJID("5511999990001", types.DefaultUserServer)
	peerB := types.NewJID("5511999990002", types.DefaultUserServer)
	ctx := context.Background()

	c.HandleOffer(ctx, offerNode("CALL1", peerA), peerA)
	defer func() { _ = c.EndCall(ctx, "CALL1", core.EndCallReasonUserEnded) }()
	c.HandleOffer(ctx, offerNode("CALL2", peerB), peerB)

	rejected := false
	for _, tag := range sock.sentInnerTags() {
		if tag == "reject" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("a new call at capacity must be rejected")
	}
	if n := c.Count(); n != 1 {
		t.Fatalf("rejected call must not be registered, count=%d", n)
	}
	if _, ok := c.Get("CALL2"); ok {
		t.Fatal("rejected call must not be gettable")
	}
}

func TestClientUnknownCallIDNoop(t *testing.T) {
	sock := &recordSock{}
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) {}, nil)
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	ctx := context.Background()

	c.HandleAccept(ctx, offerNode("GHOST", peer), peer)
	c.HandleTransport(ctx, offerNode("GHOST", peer), peer)
	c.HandleTerminate(offerNode("GHOST", peer))
	if tags := sock.sentInnerTags(); len(tags) != 0 {
		t.Fatalf("unknown-callID handlers must not send stanzas, got %v", tags)
	}
	if err := c.AcceptCall(ctx, "GHOST"); err == nil {
		t.Fatal("AcceptCall on unknown callID must error")
	}
	if err := c.RejectCall(ctx, "GHOST", core.EndCallReasonDeclined); err == nil {
		t.Fatal("RejectCall on unknown callID must error")
	}
	if err := c.EndCall(ctx, "GHOST", core.EndCallReasonUserEnded); err != nil {
		t.Fatalf("EndCall on unknown callID must be nil, got %v", err)
	}
}

func TestClientDrainAndRemove(t *testing.T) {
	sock := &recordSock{}
	mk := func() *Client {
		return NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0,
			func(string, *CallManager) {}, nil)
	}
	peerA := types.NewJID("5511999990001", types.DefaultUserServer)
	peerB := types.NewJID("5511999990002", types.DefaultUserServer)
	ctx := context.Background()

	c := mk()
	c.HandleOffer(ctx, offerNode("CALL1", peerA), peerA)
	c.HandleOffer(ctx, offerNode("CALL2", peerB), peerB)
	drained := c.Drain()
	if len(drained) != 2 {
		t.Fatalf("Drain must return all calls, got %d", len(drained))
	}
	if c.Count() != 0 {
		t.Fatalf("Drain must empty the registry, count=%d", c.Count())
	}
	for _, cm := range drained {
		_ = cm.EndCall(ctx, core.EndCallReasonUserEnded)
	}

	c2 := mk()
	c2.HandleOffer(ctx, offerNode("CALL1", peerA), peerA)
	c2.HandleOffer(ctx, offerNode("CALL2", peerB), peerB)
	defer func() { _ = c2.EndCall(ctx, "CALL2", core.EndCallReasonUserEnded) }()
	c2.Remove("CALL1")
	if _, ok := c2.Get("CALL1"); ok {
		t.Fatal("removed call must be gone")
	}
	if _, ok := c2.Get("CALL2"); !ok {
		t.Fatal("Remove must not affect other calls")
	}
	if c2.Count() != 1 {
		t.Fatalf("count after Remove must be 1, got %d", c2.Count())
	}
}
