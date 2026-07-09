package call

import (
	"context"
	"log/slog"
	"sync"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
	"wacalls/internal/voip/signaling"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type Client struct {
	sock           core.VoipSocket
	log            *slog.Logger
	makeExtensions func() []engine.Extension
	onCall         func(callID string, cm *CallManager)
	maxCalls       int
	mu             sync.Mutex
	calls          map[string]*CallManager
}

func NewClient(sock core.VoipSocket, log *slog.Logger, makeExtensions func() []engine.Extension, maxCalls int, onCall func(callID string, cm *CallManager)) *Client {
	return &Client{sock: sock, log: log, makeExtensions: makeExtensions, onCall: onCall, maxCalls: maxCalls, calls: map[string]*CallManager{}}
}

func (c *Client) createCall(callID string) *CallManager {
	cm := NewCallManager(c.sock, c.log, c.makeExtensions()...)
	c.onCall(callID, cm)
	c.mu.Lock()
	c.calls[callID] = cm
	c.mu.Unlock()
	return cm
}

func (c *Client) get(callID string) (*CallManager, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cm, ok := c.calls[callID]
	return cm, ok
}

func (c *Client) Get(callID string) (*CallManager, bool) {
	return c.get(callID)
}

func (c *Client) Remove(callID string) {
	c.mu.Lock()
	delete(c.calls, callID)
	c.mu.Unlock()
}

func (c *Client) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *Client) Drain() []*CallManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*CallManager, 0, len(c.calls))
	for _, cm := range c.calls {
		out = append(out, cm)
	}
	c.calls = map[string]*CallManager{}
	return out
}

func (c *Client) StartCall(ctx context.Context, peer types.JID, isVideo bool) (string, error) {
	callID := signaling.GenerateCallID()
	cm := c.createCall(callID)
	if err := cm.StartCall(ctx, callID, peer, isVideo); err != nil {
		c.Remove(callID)
		return "", err
	}
	return callID, nil
}

func (c *Client) HandleOffer(ctx context.Context, node *waBinary.Node, peer types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil || info.CallID == "" {
		return
	}
	if c.maxCalls > 0 && c.Count() >= c.maxCalls {
		c.reject(ctx, node, peer)
		return
	}
	cm := c.createCall(info.CallID)
	cm.HandleCallOffer(ctx, node, peer)
}

func (c *Client) reject(ctx context.Context, node *waBinary.Node, peer types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
	if creator == "" {
		creator = peer.String()
	}
	reject := signaling.BuildRejectStanza(peer, info.CallID, wanode.MustJID(creator))
	_ = c.sock.SendNode(ctx, reject)
	c.log.Info("inbound call rejected: session at capacity", "call_id", info.CallID)
}

func (c *Client) HandleAccept(ctx context.Context, node *waBinary.Node, peer types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	if cm, ok := c.get(info.CallID); ok {
		cm.HandleCallAccept(ctx, node, peer)
	}
}

func (c *Client) HandleTransport(ctx context.Context, node *waBinary.Node, peer types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	if cm, ok := c.get(info.CallID); ok {
		cm.HandleCallTransport(ctx, node, peer)
	}
}

func (c *Client) HandleTerminate(node *waBinary.Node) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	if cm, ok := c.get(info.CallID); ok {
		cm.HandleCallTerminate(node)
	}
}

func (c *Client) AcceptCall(ctx context.Context, callID string) error {
	if cm, ok := c.get(callID); ok {
		return cm.AcceptCall(ctx, callID)
	}
	return &CallError{"no call with id " + callID}
}

func (c *Client) RejectCall(ctx context.Context, callID string, reason core.EndCallReason) error {
	if cm, ok := c.get(callID); ok {
		return cm.RejectCall(ctx, callID, reason)
	}
	return &CallError{"no call with id " + callID}
}

func (c *Client) EndCall(ctx context.Context, callID string, reason core.EndCallReason) error {
	if cm, ok := c.get(callID); ok {
		return cm.EndCall(ctx, reason)
	}
	return nil
}
