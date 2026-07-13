package call

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"

	"go.mau.fi/whatsmeow/types"
)

type endRecordingObserver struct {
	core.NopObserver
	mu   sync.Mutex
	ends []string
}

func (o *endRecordingObserver) End(result, reason string) {
	o.mu.Lock()
	o.ends = append(o.ends, result+"/"+reason)
	o.mu.Unlock()
}

func (o *endRecordingObserver) endCalls() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.ends...)
}

func newEndTestClient(obs *endRecordingObserver) *Client {
	return NewClient(&recordSock{}, slog.Default(), func() []engine.Extension { return nil }, 0,
		func(string, *CallManager) {}, func(string) core.CallObserver { return obs })
}

func TestObserverEndFiresOnceOnTerminate(t *testing.T) {
	obs := &endRecordingObserver{}
	c := newEndTestClient(obs)
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	cm, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("call must register")
	}

	c.HandleTerminate(offerNode("CALL1", peer))
	cm.cleanupMedia()

	if got := obs.endCalls(); len(got) != 1 || got[0] != "failed/user_ended" {
		t.Fatalf("End calls = %v, want exactly one failed/user_ended", got)
	}
}

func TestObserverEndReportsCompletedAfterConnect(t *testing.T) {
	obs := &endRecordingObserver{}
	c := newEndTestClient(obs)
	peer := types.NewJID("5511999990000", types.DefaultUserServer)
	c.HandleOffer(context.Background(), offerNode("CALL1", peer), peer)
	cm, ok := c.Get("CALL1")
	if !ok {
		t.Fatal("call must register")
	}

	now := time.Now()
	cm.mu.Lock()
	cm.currentCall.StateData.ConnectedAt = &now
	cm.mu.Unlock()

	c.HandleTerminate(offerNode("CALL1", peer))

	if got := obs.endCalls(); len(got) != 1 || got[0] != "completed/user_ended" {
		t.Fatalf("End calls = %v, want exactly one completed/user_ended", got)
	}
}
