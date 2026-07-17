package call

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/signaling"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type queriedStanza struct {
	tag        string
	to         string
	ctxErr     error
	childAttrs waBinary.Attrs
}

type ctxQuerySock struct {
	fakeSock
	mu      sync.Mutex
	queries []queriedStanza
	done    chan struct{}
}

func newCtxQuerySock() *ctxQuerySock {
	return &ctxQuerySock{done: make(chan struct{}, 4)}
}

func (s *ctxQuerySock) Query(ctx context.Context, node waBinary.Node) (*waBinary.Node, error) {
	tag := ""
	var childAttrs waBinary.Attrs
	if children := wanode.NodeChildren(&node); len(children) > 0 {
		tag = children[0].Tag
		childAttrs = children[0].Attrs
	}
	to := ""
	if j, ok := node.Attrs["to"].(types.JID); ok {
		to = j.String()
	}
	s.mu.Lock()
	s.queries = append(s.queries, queriedStanza{tag: tag, to: to, ctxErr: ctx.Err(), childAttrs: childAttrs})
	s.mu.Unlock()
	s.done <- struct{}{}
	return nil, nil
}

func (s *ctxQuerySock) queried() []queriedStanza {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]queriedStanza(nil), s.queries...)
}

func awaitQuery(t *testing.T, s *ctxQuerySock) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stanza never reached the socket")
	}
}

func ringingManager(t *testing.T, sock signaling.Socket) *CallManager {
	t.Helper()
	c := NewClient(sock, slog.Default(), func() []engine.Extension { return nil }, 0, func(string, *CallManager) {}, nil)
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	cm, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("offer must register a call manager")
	}
	return cm
}

func TestRejectCallSurvivesCanceledContext(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cm.RejectCall(ctx, "CALL1", core.EndCallReasonDeclined); err != nil {
		t.Fatalf("reject of ringing call: %v", err)
	}

	awaitQuery(t, sock)
	q := sock.queried()
	if len(q) != 1 || q[0].tag != "reject" {
		t.Fatalf("want exactly one reject stanza, got %+v", q)
	}
	if q[0].ctxErr != nil {
		t.Fatalf("reject must go out detached from the caller context, Query saw ctx err: %v", q[0].ctxErr)
	}
}

func TestEndCallSurvivesCanceledContext(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cm.EndCall(ctx, core.EndCallReasonUserEnded); err != nil {
		t.Fatalf("end call: %v", err)
	}

	awaitQuery(t, sock)
	q := sock.queried()
	if len(q) != 1 || q[0].tag != "terminate" {
		t.Fatalf("want exactly one terminate stanza, got %+v", q)
	}
	if q[0].ctxErr != nil {
		t.Fatalf("terminate must go out detached from the caller context, Query saw ctx err: %v", q[0].ctxErr)
	}
}

func TestEndCallTerminatesToAnsweringDevice(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	// The peer answered on a companion device (e.g. WhatsApp Web :33); the terminate must target
	// that device, not the base JID, or the companion never sees it and hangs in reconnecting.
	answering := "62440234549366:33@lid"
	cm.mu.Lock()
	cm.acceptedByJid = answering
	cm.mu.Unlock()

	if err := cm.EndCall(context.Background(), core.EndCallReasonUserEnded); err != nil {
		t.Fatalf("end call: %v", err)
	}
	awaitQuery(t, sock)
	q := sock.queried()
	if len(q) != 1 || q[0].tag != "terminate" {
		t.Fatalf("want exactly one terminate stanza, got %+v", q)
	}
	if q[0].to != answering {
		t.Fatalf("terminate must target the answering device %q, got to=%q", answering, q[0].to)
	}
}

func TestEndCallTerminatesToPeerWhenNoAnswerDevice(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	// No accept device recorded (inbound call, or ended before accept): fall back to call.PeerJid.
	want := cm.CurrentCall().PeerJid
	if want == "" {
		t.Fatal("call must have a peer jid")
	}

	if err := cm.EndCall(context.Background(), core.EndCallReasonUserEnded); err != nil {
		t.Fatalf("end call: %v", err)
	}
	awaitQuery(t, sock)
	q := sock.queried()
	if len(q) != 1 || q[0].tag != "terminate" {
		t.Fatalf("want exactly one terminate stanza, got %+v", q)
	}
	if q[0].to != want {
		t.Fatalf("terminate must fall back to the peer jid %q, got to=%q", want, q[0].to)
	}
}

func TestRejectCallInvalidStateSendsNothing(t *testing.T) {
	sock := newCtxQuerySock()
	cm := ringingManager(t, sock)

	cm.mu.Lock()
	if err := cm.currentCall.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		cm.mu.Unlock()
		t.Fatalf("accept: %v", err)
	}
	if err := cm.currentCall.ApplyTransition(Transition{Type: TransitionMediaConnected}); err != nil {
		cm.mu.Unlock()
		t.Fatalf("connect: %v", err)
	}
	cm.mu.Unlock()

	err := cm.RejectCall(context.Background(), "CALL1", core.EndCallReasonDeclined)
	var invalid *InvalidTransition
	if !errors.As(err, &invalid) {
		t.Fatalf("reject of active call must return InvalidTransition, got %v", err)
	}
	if !cm.CurrentCall().IsActive() {
		t.Fatal("active call must stay active after an invalid reject")
	}

	if err := cm.EndCall(context.Background(), core.EndCallReasonUserEnded); err != nil {
		t.Fatalf("end call: %v", err)
	}
	awaitQuery(t, sock)
	for _, q := range sock.queried() {
		if q.tag == "reject" {
			t.Fatal("invalid reject must not reach the wire")
		}
	}
}
