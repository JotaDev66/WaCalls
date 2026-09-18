# Plan 002: Close the TOCTOU race that lets one operator start two calls

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3d2279e..HEAD -- cmd/server/httpapi.go cmd/server/broker.go`
> and `git status --short -- cmd/server/httpapi.go cmd/server/broker.go`.
> Both files are clean (no uncommitted local changes) as of the time this
> plan was written. If either command shows changes, compare the "Current
> state" excerpts below against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (touches files shared with plans 001/003 — see `plans/README.md` dependency notes for suggested sequencing)
- **Category**: bug
- **Planned at**: commit `3d2279e`, 2026-09-17

## Why this matters

`doStartCall` is meant to enforce "one active call per operator" — it
checks `s.broker.ownerActiveCall(owner)` before starting a call. But the
check happens, then real network I/O to WhatsApp runs
(`sess.startOutgoing`, which sends a call offer), and only *after that
succeeds* does the code register the call under `owner`
(`s.broker.upsertCall`). Two concurrent `POST /api/sessions/{sid}/calls`
requests with the same `X-Client-Id`/`clientId` can both pass the
"not already on a call" check before either has registered — nothing
blocks it, because the registration that the check relies on doesn't
happen until after the slow network call. The result: one operator ends up
with two live outbound WhatsApp calls, but the UI and the rest of the
system (broker owner tracking, the one-call-per-tab assumption elsewhere)
only expect one, so the second call becomes an orphaned, unreachable call
that nothing will ever accept, reject, or end.

## Current state

- `cmd/server/httpapi.go` — `doStartCall`, the affected handler.
- `cmd/server/broker.go` — `Broker`, needs one new field and two new
  methods to make the claim atomic.

`cmd/server/httpapi.go:160-197` — `doStartCall` as it exists today:

```go
func (s *server) doStartCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Phone      string `json:"phone"`
		DurationMs int    `json:"duration_ms"`
		Record     bool   `json:"record"`
		Video      bool   `json:"video"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Phone) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone required"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	if max := s.sessions.maxCalls; max > 0 && sess.reg.count() >= max {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "max concurrent calls"})
		return
	}
	peer := types.NewJID(normalizePhone(body.Phone), types.DefaultUserServer)

	callID, err := sess.startOutgoing(r.Context(), peer, body.Video)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	callMedia := "audio"
	if body.Video {
		callMedia = "video"
	}
	s.broker.upsertCall(CallRecord{
		SessionID: sess.id, CallID: callID, Owner: &owner, Direction: "outbound", Peer: peer.String(),
		Media: callMedia, StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
	})
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": callID}})
}
```

`cmd/server/broker.go:51-65` — `Broker` struct and constructor (the field
you'll add goes here):

```go
type Broker struct {
	mu      sync.RWMutex
	subs    map[*subscriber]struct{}
	calls   map[string]*CallRecord
	history []CallRecord

	SnapshotFn func() []any
}

func NewBroker() *Broker {
	return &Broker{
		subs:  map[*subscriber]struct{}{},
		calls: map[string]*CallRecord{},
	}
}
```

`cmd/server/broker.go:149-160` — `ownerActiveCall`, the existing check this
plan's new method must incorporate (an operator with an already-registered
active call must still be rejected, reservation is *in addition to* this,
not instead of it):

```go
func (b *Broker) ownerActiveCall(owner string) string {
	if owner == "" {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for id, c := range b.calls {
		if c.Owner != nil && *c.Owner == owner && c.Status != StatusEnded {
			return id
		}
	}
	return ""
}
```

**Why callID can't be reserved directly**: `sess.startOutgoing`
(`cmd/server/session.go:123-131`) generates the `callID` *internally* via
`signaling.GenerateCallID()` before it's known to `httpapi.go` — so the
reservation must be keyed by `owner`, not by a not-yet-known `callID`, and
released once the real `CallRecord` (keyed by the real `callID`) exists or
the attempt fails.

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
- `cmd/server/broker.go` (add `reserved` field + `tryReserveOwner`/`releaseReservation` methods)
- `cmd/server/httpapi.go` (`doStartCall` only)
- `cmd/server/broker_test.go` (add tests)

**Out of scope**:
- `cmd/server/session.go` / `startOutgoing` — do not change its signature
  or make it callID-aware earlier; the fix belongs entirely in the
  owner-reservation layer.
- `doAccept`'s existing `ownerActiveCall`/`setOwner` logic — already
  correct for its own purpose (claiming an *inbound* call), do not merge
  it with this outbound-specific reservation.
- Plan 001 (ownership checks on webrtc/reject/end) and plan 003 (`ac.bridge`
  data race) — separate concerns, do not fix here.

## Git workflow

- Branch: `advisor/002-start-call-owner-reservation` (create from
  `feature/video-nack-e-keyframe`).
- Commit message style: conventional commits, Portuguese scope/summary —
  e.g. `fix(call): fecha corrida entre check de owner e criação da chamada`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add the reservation set to `Broker`

In `cmd/server/broker.go`, add a field to the struct and initialize it:

```go
type Broker struct {
	mu       sync.RWMutex
	subs     map[*subscriber]struct{}
	calls    map[string]*CallRecord
	reserved map[string]struct{} // owner -> in-flight outbound-call reservation
	history  []CallRecord

	SnapshotFn func() []any
}

func NewBroker() *Broker {
	return &Broker{
		subs:     map[*subscriber]struct{}{},
		calls:    map[string]*CallRecord{},
		reserved: map[string]struct{}{},
	}
}
```

**Verify**: `go build ./cmd/server/...` → exit 0.

### Step 2: Add `tryReserveOwner` and `releaseReservation`

Add these two methods near `ownerActiveCall` in `cmd/server/broker.go`.
`tryReserveOwner` must check both the existing active-call map *and* the
reservation set, atomically under one lock (this is what closes the race —
the check and the reservation happen inside the same critical section):

```go
// tryReserveOwner atomically checks that owner has no active call and no
// in-flight reservation, and if so reserves owner for an outbound call
// being started. Call releaseReservation(owner) exactly once, however the
// attempt ends (success or failure).
func (b *Broker) tryReserveOwner(owner string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if owner == "" {
		return false
	}
	if _, reserved := b.reserved[owner]; reserved {
		return false
	}
	for _, c := range b.calls {
		if c.Owner != nil && *c.Owner == owner && c.Status != StatusEnded {
			return false
		}
	}
	b.reserved[owner] = struct{}{}
	return true
}

func (b *Broker) releaseReservation(owner string) {
	b.mu.Lock()
	delete(b.reserved, owner)
	b.mu.Unlock()
}
```

**Verify**: `go build ./cmd/server/...` → exit 0.

### Step 3: Wire it into `doStartCall`

Replace the `ownerActiveCall` check with `tryReserveOwner`, and release the
reservation on every exit path after the reservation is acquired (use
`defer` right after a successful reservation so it covers the `maxCalls`
rejection, the `startOutgoing` error, and the success path uniformly):

```go
owner := clientID(r)
if !s.broker.tryReserveOwner(owner) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
	return
}
defer s.broker.releaseReservation(owner)

if max := s.sessions.maxCalls; max > 0 && sess.reg.count() >= max {
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "max concurrent calls"})
	return
}
peer := types.NewJID(normalizePhone(body.Phone), types.DefaultUserServer)

callID, err := sess.startOutgoing(r.Context(), peer, body.Video)
if err != nil {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	return
}
callMedia := "audio"
if body.Video {
	callMedia = "video"
}
s.broker.upsertCall(CallRecord{
	SessionID: sess.id, CallID: callID, Owner: &owner, Direction: "outbound", Peer: peer.String(),
	Media: callMedia, StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
})
writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": callID}})
```

Note the `defer` releases the reservation *after* `upsertCall` has already
registered the real `CallRecord` with `Owner` set — from that point on,
`ownerActiveCall`/the loop inside `tryReserveOwner` sees the real record,
so releasing the reservation at that point is safe and doesn't reopen the
race.

**Verify**: `go build ./cmd/server/...` → exit 0; `gofmt -l cmd/server/broker.go cmd/server/httpapi.go` → empty.

### Step 4: Write a concurrency test

Add to `cmd/server/broker_test.go`: two goroutines calling
`tryReserveOwner("op-A")` concurrently must have exactly one succeed. Model
the structure on the existing `TestOwnerActiveCall` in the same file (plain
`*testing.T`, no extra helpers beyond `ownerPtr` which already exists
there).

```go
func TestTryReserveOwnerIsExclusive(t *testing.T) {
	b := NewBroker()
	results := make(chan bool, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- b.tryReserveOwner("op-A")
		}()
	}
	close(start)
	a, c := <-results, <-results
	if a == c {
		t.Fatalf("exactly one reservation must succeed, got %v and %v", a, c)
	}
}

func TestTryReserveOwnerBlocksActiveCall(t *testing.T) {
	b := NewBroker()
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Owner: ownerPtr("op-A"), Status: StatusConnected})
	if b.tryReserveOwner("op-A") {
		t.Fatal("must not reserve for an owner with an already-active call")
	}
}

func TestReleaseReservationAllowsRetry(t *testing.T) {
	b := NewBroker()
	if !b.tryReserveOwner("op-A") {
		t.Fatal("first reservation should succeed")
	}
	if b.tryReserveOwner("op-A") {
		t.Fatal("second reservation while first is held must fail")
	}
	b.releaseReservation("op-A")
	if !b.tryReserveOwner("op-A") {
		t.Fatal("reservation should be available again after release")
	}
}
```

**Verify**: `go test ./cmd/server/... -run TestTryReserveOwner -v` and `-run TestReleaseReservationAllowsRetry -v` → all pass. Also run `go test -race ./cmd/server/... -run TestTryReserveOwnerIsExclusive` → no race reported.

## Test plan

- `cmd/server/broker_test.go`: the three tests in Step 4
  (exclusivity under concurrency, blocks when an active call already
  exists, release allows retry).
- Full suite with the race detector on the package that changed:
  `go test -race ./cmd/server/...` → all pass, no data races reported.
- Full repo: `go test ./...` → all pass.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `gofmt -l .` is empty
- [ ] `go test -race ./cmd/server/...` exits 0, no race reported
- [ ] `go test ./...` exits 0, including the three new broker tests
- [ ] `doStartCall` no longer calls `s.broker.ownerActiveCall` directly (calls `tryReserveOwner` instead)
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row for 002 updated

## STOP conditions

- The code at `cmd/server/httpapi.go`/`broker.go` doesn't match the
  excerpts in "Current state" — re-read and compare before proceeding.
- A step's verification fails twice after a reasonable fix attempt.
- You find `sess.startOutgoing` can itself be called from another path that
  also needs reservation (grep `startOutgoing` call sites first) — if so,
  STOP and report rather than silently reservation-guarding only
  `doStartCall`.
- `go test -race` reports any race not already present on `main` before
  this change — investigate before assuming it's pre-existing.

## Maintenance notes

- If a future endpoint starts calls another way (e.g. a bulk/dial-out
  API), it must also go through `tryReserveOwner`/`releaseReservation`, or
  the race reopens for that path.
- A reviewer should check the `defer` placement carefully: it must be
  registered immediately after a *successful* `tryReserveOwner`, and must
  fire on every subsequent return (the `maxCalls` rejection, the
  `startOutgoing` error, and the success path) — a `defer` placed too late
  (e.g. after the `maxCalls` check) would reopen the race for the skipped
  early-return path.
- Plans 001 and 003 touch overlapping files (`httpapi.go`, `broker.go`,
  `session.go`) — no logical dependency on this plan, but rebase/merge
  carefully if executed in the same working tree.
