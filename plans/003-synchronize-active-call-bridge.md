# Plan 003: Synchronize access to activeCall.bridge

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3d2279e..HEAD -- cmd/server/callregistry.go cmd/server/session.go`
> and `git status --short -- cmd/server/callregistry.go cmd/server/session.go`.
> Both files are clean (no uncommitted local changes) as of the time this
> plan was written. If either command shows changes, compare the "Current
> state" excerpts below against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (touches files shared with plans 001/002 — see `plans/README.md` dependency notes for suggested sequencing)
- **Category**: bug
- **Planned at**: commit `3d2279e`, 2026-09-17

## Why this matters

`activeCall.bridge` is mutated under `callRegistry.mu`
(`cmd/server/callregistry.go`'s `setBridge`), but it is read directly —
with no lock at all — from three media-callback closures in
`cmd/server/session.go` (`OnPeerAudio`, `OnPeerVideo`, `OnKeyframeRequest`).
Those closures run on WhatsApp media-delivery goroutines and can execute
concurrently with an HTTP handler calling `doWebRTC` →
`sess.setBridge` → `callRegistry.setBridge`, which reassigns the same
field. This is a real, unsynchronized concurrent read/write on a shared
pointer field — exactly what Go's race detector exists to catch (this repo
already runs `go test -race` in CI's server job). In combination with plan
001 (any LAN client can currently re-POST `/webrtc` mid-call), this isn't
theoretical: a fresh `Bridge` can be swapped in concurrently with in-flight
`OnPeerAudio`/`OnPeerVideo` delivery, and the unsynchronized read can
observe a torn or stale pointer.

## Current state

- `cmd/server/callregistry.go` — owns `activeCall.bridge` and the mutex
  that's supposed to guard it.
- `cmd/server/session.go` — reads `ac.bridge` directly in three places
  without going through the registry's lock.

`cmd/server/callregistry.go` (full file, current state):

```go
package main

import (
	"sync"

	"wacalls/internal/voip/call"
)

type activeCall struct {
	cm     *call.CallManager
	bridge *Bridge
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
```

`cmd/server/session.go:91-112` — the three unsynchronized read sites this
plan must fix:

```go
cm.OnPeerAudio = func(pcm16 []float32) {
	ac, ok := s.reg.get(callID)
	if !ok || ac.bridge == nil {
		return
	}
	_ = ac.bridge.WritePCM(pcm16)
}
cm.OnPeerVideo = func(f media.VideoFrame) {
	ac, ok := s.reg.get(callID)
	if !ok || ac.bridge == nil {
		return
	}
	_ = ac.bridge.WriteVideo(f)
}
cm.OnKeyframeRequest = func() {
	ac, ok := s.reg.get(callID)
	if !ok || ac.bridge == nil {
		return
	}
	_ = ac.bridge.RequestKeyframe()
}
```

**Two other `ac.bridge` reads exist and are intentionally out of scope —
do not change them:**

`cmd/server/session.go:266-274` (`removeCall`) and `cmd/server/session.go:284-291`
(`teardownAllCalls`) also read `ac.bridge`, but both do so *after* calling
`s.reg.remove(callID)` / `s.reg.drain()` — at that point the `activeCall`
has already been removed from the registry's map under the registry lock,
so no concurrent `setBridge` can reach it anymore (`setBridge` looks the
call up by ID in the same map). These two are not part of the race; leave
them reading `ac.bridge` directly.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Vet | `go vet ./...` | exit 0 |
| Format check | `gofmt -l .` | empty output |
| Build | `go build ./...` | exit 0 |
| Test (race-sensitive) | `go test -race ./cmd/server/...` | all pass, no race reported |
| Test (whole repo) | `go test ./...` | all pass |

## Scope

**In scope**:
- `cmd/server/callregistry.go` (add a `bridge(callID string) (*Bridge, bool)` method)
- `cmd/server/session.go` (`OnPeerAudio`, `OnPeerVideo`, `OnKeyframeRequest` only)
- `cmd/server/callregistry_test.go` (add a test)

**Out of scope**:
- `cmd/server/session.go`'s `removeCall` and `teardownAllCalls` — their
  `ac.bridge` reads happen post-removal and are not racy (see "Current
  state" above); do not touch them.
- `ac.cm` field access anywhere — it's set once at construction
  (`createCall`) and never reassigned, so it's not part of this race.
- Plans 001 (ownership checks) and 002 (start-call reservation) — separate
  concerns, do not fix here even though they touch nearby files.

## Git workflow

- Branch: `advisor/003-synchronize-active-call-bridge` (create from
  `feature/video-nack-e-keyframe`).
- Commit message style: conventional commits, Portuguese scope/summary —
  e.g. `fix(call): sincroniza acesso a activeCall.bridge`.
- Do NOT push or open a PR unless the operator instructed it.

### Step 1: Add a locked bridge accessor to `callRegistry`

In `cmd/server/callregistry.go`, add this method (place it next to
`setBridge`, same style):

```go
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
```

**Verify**: `go build ./cmd/server/...` → exit 0.

### Step 2: Use it from the three media callbacks

In `cmd/server/session.go`, replace the three closures in `wireCall`:

```go
cm.OnPeerAudio = func(pcm16 []float32) {
	b, ok := s.reg.bridge(callID)
	if !ok {
		return
	}
	_ = b.WritePCM(pcm16)
}
cm.OnPeerVideo = func(f media.VideoFrame) {
	b, ok := s.reg.bridge(callID)
	if !ok {
		return
	}
	_ = b.WriteVideo(f)
}
cm.OnKeyframeRequest = func() {
	b, ok := s.reg.bridge(callID)
	if !ok {
		return
	}
	_ = b.RequestKeyframe()
}
```

Do not change `removeCall` or `teardownAllCalls` (see "Current state" for
why they're out of scope).

**Verify**: `go build ./cmd/server/...` → exit 0; `gofmt -l cmd/server/callregistry.go cmd/server/session.go` → empty.

### Step 3: Write a race test

Add to `cmd/server/callregistry_test.go`, modeled on the existing
`TestCallRegistrySetBridgeMissing` in the same file: concurrently call
`setBridge` and `bridge` on the same `callID` and confirm `go test -race`
reports nothing.

```go
func TestCallRegistryBridgeConcurrentAccess(t *testing.T) {
	r := newCallRegistry()
	r.add("c1", &activeCall{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			r.setBridge("c1", &Bridge{})
		}
	}()
	for i := 0; i < 100; i++ {
		r.bridge("c1")
	}
	<-done
}

func TestCallRegistryBridgeMissing(t *testing.T) {
	r := newCallRegistry()
	r.add("c1", &activeCall{})
	if _, ok := r.bridge("c1"); ok {
		t.Fatal("bridge should report not-found when no bridge is set yet")
	}
	b := &Bridge{}
	r.setBridge("c1", b)
	got, ok := r.bridge("c1")
	if !ok || got != b {
		t.Fatalf("bridge should return the set bridge: ok=%v got=%v", ok, got)
	}
}
```

**Verify**: `go test -race ./cmd/server/... -run TestCallRegistryBridge -v` → all pass, no race reported.

## Test plan

- `cmd/server/callregistry_test.go`: the two tests in Step 3 (concurrent
  access under `-race`, and correctness of `bridge`'s not-found/found
  cases).
- `go test -race ./cmd/server/...` → all pass, no data race reported
  anywhere in the package (not just the new tests — the point is that the
  race is gone repo-wide for this field).
- Full repo: `go test ./...` → all pass.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `gofmt -l .` is empty
- [ ] `go test -race ./cmd/server/...` exits 0, no race reported
- [ ] `go test ./...` exits 0, including the two new callregistry tests
- [ ] `grep -n "ac.bridge" cmd/server/session.go` shows only the two
      post-removal reads in `removeCall`/`teardownAllCalls` — the three
      `OnPeer*`/`OnKeyframeRequest` closures no longer reference `ac.bridge` directly
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row for 003 updated

## STOP conditions

- The code at `cmd/server/callregistry.go`/`session.go` doesn't match the
  excerpts in "Current state" — re-read and compare before proceeding.
- A step's verification fails twice after a reasonable fix attempt.
- `go test -race` reports a *different* race than the one this plan fixes
  — report it rather than silently also trying to fix it (out of scope for
  this plan).
- You find another direct `ac.bridge` (or `ac.cm`) access outside the five
  sites named in this plan (grep `\.bridge\b` and `\.cm\b` in
  `cmd/server/*.go` to confirm) — if so, STOP and report the additional
  site instead of guessing whether it needs the same fix.

## Maintenance notes

- Any future code that reads a call's bridge must go through
  `callRegistry.bridge(callID)`, never `ac.bridge` directly (except the two
  already-safe post-removal reads called out above) — this is now the
  established pattern.
- A reviewer should check that no new direct `ac.bridge` read was
  introduced elsewhere by mistake during this change (`grep -n "ac.bridge"
  cmd/server/*.go` should only show the constructor, `setBridge`, and the
  two post-removal sites).
- Plans 001 and 002 touch overlapping files (`httpapi.go`, `broker.go`,
  `session.go`) — no logical dependency on this plan, but rebase/merge
  carefully if executed in the same working tree.
