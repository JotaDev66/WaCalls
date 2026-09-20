package main

import (
	"sync"
	"sync/atomic"

	meowcaller "github.com/purpshell/meowcaller"
)

type activeCall struct {
	call   *meowcaller.Call
	src    *liveAudioSource
	bridge *Bridge
	// videoSink é registrado em Call.ReceiveVideo já em wireCall, antes do
	// bridge WebRTC do browser existir — ver o comentário em
	// orientedVideoSink (bridge.go) pra saber por quê.
	videoSink *orientedVideoSink
	// held controla se bridge.OnBrowserPCM empurra pra src — enquanto true,
	// os quadros do microfone do browser são descartados em vez de
	// bufferizados, então o unhold retoma ao vivo em vez de tocar um atraso
	// acumulado (ver doHold/doUnhold).
	held atomic.Bool
}

type callRegistry struct {
	mu    sync.Mutex
	calls map[string]*activeCall
}

func newCallRegistry() *callRegistry {
	return &callRegistry{calls: map[string]*activeCall{}}
}

func (r *callRegistry) add(callID string, ac *activeCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[callID] = ac
}

func (r *callRegistry) get(callID string) (*activeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	return ac, ok
}

func (r *callRegistry) remove(callID string) (*activeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	if !ok {
		return nil, false
	}
	delete(r.calls, callID)
	return ac, true
}

func (r *callRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *callRegistry) setBridge(callID string, b *Bridge) (*Bridge, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	if !ok {
		return nil, false
	}
	oldB := ac.bridge
	ac.bridge = b
	return oldB, true
}

// bridge returns the call's current bridge under the registry lock, safe
// to call concurrently with setBridge.
func (r *callRegistry) bridge(callID string) (*Bridge, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	if !ok || ac.bridge == nil {
		return nil, false
	}
	return ac.bridge, true
}

func (r *callRegistry) drain() []*activeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*activeCall, 0, len(r.calls))
	for _, ac := range r.calls {
		out = append(out, ac)
	}
	r.calls = map[string]*activeCall{}
	return out
}
