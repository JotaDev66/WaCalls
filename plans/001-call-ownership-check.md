# Plan 001: Enforce call ownership on webrtc/reject/end endpoints

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
- **Depends on**: none (touches files shared with plans 002/003 — see `plans/README.md` dependency notes for suggested sequencing)
- **Category**: security
- **Planned at**: commit `3d2279e`, 2026-09-17

## Why this matters

WaCalls's HTTP API is intentionally unauthenticated and LAN-only (documented
in `README.md`'s "## Segurança" section — do not "fix" that, it's a
deliberate, accepted tradeoff and out of scope here). But *within* that
LAN-trust model, the code already establishes a narrower invariant: a call
has one `Owner` (the browser tab/client that claimed it), and `doAccept`
enforces it. Three sibling endpoints that mutate the same call —
`doWebRTC`, `doReject`, `doEndCall` — do not check ownership at all. Any
client on the LAN that knows (or observes via the unauthenticated
`GET /api/events` SSE stream, which broadcasts call status to everyone) a
`sid`+`callID` can re-POST `/webrtc` mid-call to swap out the legitimate
operator's WebRTC bridge (silently evicting them from audio/video), or
unilaterally end/reject a call another operator is actively handling. This
is a logic gap next to code that clearly intends ownership to matter, not a
restatement of the documented "no auth" tradeoff.

## Current state

- `cmd/server/httpapi.go` — HTTP handlers for the call lifecycle.
- `cmd/server/broker.go` — in-memory call registry (`Broker`) that already
  tracks `CallRecord.Owner` and has the primitives this plan needs.

`cmd/server/broker.go:19-29` — `CallRecord` has an `Owner *string` field:

```go
type CallRecord struct {
	SessionID string     `json:"sessionId"`
	CallID    string     `json:"callId"`
	Owner     *string    `json:"owner"`
	Direction string     `json:"direction"`
	Peer      string     `json:"peer"`
	Media     string     `json:"media,omitempty"`
	StartedAt int64      `json:"startedAt"`
	Status    CallStatus `json:"status"`
	EndedAt   *int64     `json:"endedAt,omitempty"`
	EndReason string     `json:"endReason,omitempty"`
}
```

`cmd/server/broker.go:124-133` — `getCall` already exists and returns a
safe copy (no lock needed by the caller):

```go
func (b *Broker) getCall(id string) (*CallRecord, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.calls[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}
```

`cmd/server/httpapi.go:236-249` — `doAccept` is the one endpoint that
**does** enforce ownership today (model the fix on this pattern):

```go
func (s *server) doAccept(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" && other != id {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	if !s.broker.setOwner(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "claimed by another client"})
		return
	}
	...
}
```

`cmd/server/httpapi.go:60-65` — `clientID(r)` extracts the caller's
self-declared client id (header `X-Client-Id` or `?clientId=`):

```go
func clientID(r *http.Request) string {
	if id := r.Header.Get("X-Client-Id"); id != "" {
		return id
	}
	return r.URL.Query().Get("clientId")
}
```

`cmd/server/httpapi.go:200-278` — the three endpoints this plan fixes, as
they exist today (no ownership check):

```go
func (s *server) doWebRTC(sess *Session, w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	ac, ok := sess.reg.get(callID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	var body struct {
		SDPOffer string `json:"sdp_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SDPOffer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp_offer required"})
		return
	}
	bridge, answer, err := NewBridge(body.SDPOffer, s.log)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	bridge.OnBrowserPCM = func(pcm []float32) { ac.cm.FeedCapturedPCM(pcm) }
	bridge.OnBrowserVideo = func(f media.VideoFrame) { ac.cm.FeedCapturedVideo(f) }
	bridge.OnTerminalICE = func() { go sess.terminateCall(callID, core.EndCallReasonUserEnded) }
	sess.setBridge(callID, bridge)
	writeJSON(w, http.StatusOK, map[string]string{"sdp_answer": answer})
}

func (s *server) doReject(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ac, ok := sess.reg.get(id); ok {
		_ = ac.cm.RejectCall(r.Context(), id, core.EndCallReasonDeclined)
	}
	sess.removeCall(id)
	s.broker.endCall(id, string(core.EndCallReasonDeclined))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doEndCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ac, ok := sess.reg.get(id); ok {
		_ = ac.cm.EndCall(r.Context(), core.EndCallReasonUserEnded)
	}
	sess.removeCall(id)
	s.broker.endCall(id, string(core.EndCallReasonUserEnded))
	w.WriteHeader(http.StatusNoContent)
}
```

**Important nuance — unclaimed calls must still work.** A freshly-created
outbound call (`doStartCall`) already sets `Owner` at creation
(`cmd/server/httpapi.go:194-196`, unchanged by this plan). An **inbound**
call, however, starts with `Owner == nil` (see `cmd/server/session.go:60-64`,
`OnIncoming` calls `upsertCall` with no `Owner` set) until some client calls
`doAccept`. But the browser also calls `doWebRTC` to set up the media
bridge — check the client code's call order before assuming `doAccept`
always precedes `doWebRTC`: read `client/src/services` (search for calls to
`/accept` and `/webrtc`) to confirm whether the browser calls `/accept`
before or after `/webrtc` for inbound calls. Your ownership check must
**allow** the call when `Owner == nil` (unclaimed — first caller claims it
implicitly, matching how `doAccept` already treats a nil owner via
`setOwner`) and only **reject** when `Owner != nil && *Owner != clientID(r)`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Vet | `go vet ./...` | exit 0 |
| Format check | `gofmt -l .` | empty output |
| Build | `go build ./...` | exit 0 |
| Test (this package) | `go test ./cmd/server/...` | all pass |
| Test (whole repo) | `go test ./...` | all pass |

## Scope

**In scope**:
- `cmd/server/httpapi.go` (`doWebRTC`, `doReject`, `doEndCall`)
- `cmd/server/httpapi_test.go` (create — package `main` has no test file for
  this file today; follow the style of `cmd/server/broker_test.go` and
  `cmd/server/callregistry_test.go`)

**Out of scope** (do NOT touch, even though related):
- The unauthenticated nature of the API itself (`withCORS`, lack of any
  auth header check) — documented, accepted tradeoff in `README.md`.
- `doAccept` and `doStartCall` — already correct, do not modify.
- `cmd/server/broker.go` — no changes needed; `getCall`/`ownerActiveCall`
  already provide what you need read-only.
- The TOCTOU race in `doStartCall` (plan 002) and the `ac.bridge` data race
  (plan 003) — separate plans, do not fix here even if you notice them.

## Git workflow

- Branch: `advisor/001-call-ownership-check` (create from the current
  branch `feature/video-nack-e-keyframe`; do not branch from `main` — this
  repo's active work is on the feature branch).
- Commit message style: conventional commits with Portuguese scope/summary,
  matching `git log` — e.g. `fix(call): valida dono da chamada em webrtc/reject/end`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add an ownership guard helper

In `cmd/server/httpapi.go`, add a small helper near `clientID`:

```go
// callOwnerAllows reports whether owner may act on call id: true if the call
// is unclaimed (Owner == nil) or owner is the current owner.
func (s *server) callOwnerAllows(id, owner string) bool {
	rec, ok := s.broker.getCall(id)
	if !ok {
		return true // no record yet; let the per-endpoint 404 handle it
	}
	return rec.Owner == nil || *rec.Owner == owner
}
```

**Verify**: `go build ./cmd/server/...` → exit 0.

### Step 2: Guard `doWebRTC`

Right after the existing `ac, ok := sess.reg.get(callID)` / not-found check,
add:

```go
owner := clientID(r)
if !s.callOwnerAllows(callID, owner) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
	return
}
```

Do not otherwise change `doWebRTC`'s body.

**Verify**: `go build ./cmd/server/...` → exit 0.

### Step 3: Guard `doReject` and `doEndCall`

Same pattern, right after `id := r.PathValue("id")` in each:

```go
owner := clientID(r)
if !s.callOwnerAllows(id, owner) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
	return
}
```

Keep the rest of both functions unchanged (still call `sess.reg.get`,
`ac.cm.RejectCall`/`EndCall`, `sess.removeCall`, `s.broker.endCall` exactly
as today).

**Verify**: `go build ./cmd/server/...` → exit 0; `gofmt -l cmd/server/httpapi.go` → empty.

### Step 4: Write ownership tests

Create `cmd/server/httpapi_test.go` (package `main`). Test
`callOwnerAllows` directly (it needs only a `*server` with a `*Broker` —
check `cmd/server/server.go` / other `_test.go` files in the package for
how a minimal `*server` is constructed in existing tests, e.g.
`bridge_test.go` or `sessionmanager_test.go`, and reuse that pattern rather
than inventing a new one):

- unclaimed call (`Owner == nil`) → `callOwnerAllows` returns `true` for any owner string.
- call owned by `"op-A"` → `callOwnerAllows(id, "op-A")` is `true`, `callOwnerAllows(id, "op-B")` is `false`.
- unknown call id → `callOwnerAllows` returns `true` (lets the endpoint's own 404 handle it).

**Verify**: `go test ./cmd/server/... -run TestCallOwnerAllows -v` → all subtests pass.

## Test plan

- New tests in `cmd/server/httpapi_test.go`, table-driven or three separate
  `func Test...` functions, covering: unclaimed call, owned-by-caller,
  owned-by-someone-else, unknown call id. Model the `Broker`/`CallRecord`
  setup on `cmd/server/broker_test.go`'s `TestOwnerActiveCall` (uses
  `NewBroker()` + `upsertCall` + the `ownerPtr` helper already defined
  there).
- Full suite: `go test ./...` → all pass, including the new tests.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `gofmt -l .` is empty
- [ ] `go test ./...` exits 0, including new `TestCallOwnerAllows*` cases
- [ ] `doWebRTC`, `doReject`, `doEndCall` all call `callOwnerAllows` before mutating state
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row for 001 updated

## STOP conditions

- The code at `cmd/server/httpapi.go` doesn't match the excerpts in
  "Current state" (drift since this plan was written) — re-read the file
  and compare before proceeding.
- You find that the browser client calls `/webrtc` **before** `/accept` for
  inbound calls (check `client/src/services`) in a way that would make an
  unclaimed-call-must-be-allowed exception insufficient — if so, STOP and
  report the actual call order instead of guessing at a fix.
- A step's verification fails twice after a reasonable fix attempt.
- The fix appears to require touching `cmd/server/broker.go` or
  `cmd/server/session.go` — it shouldn't; if it does, STOP and report why.

## Maintenance notes

- Any *new* endpoint added later that mutates an existing call (keyed by
  `callID`) should call `callOwnerAllows` the same way — this is now the
  established pattern, alongside `doAccept`'s more elaborate
  claim-and-check logic (which additionally prevents one operator from
  holding two calls at once; `callOwnerAllows` alone does not need to
  replicate that, it only guards "is this caller allowed to touch this
  specific call").
- A reviewer should specifically check: does the fix still let a *fresh*
  inbound call's `doWebRTC` succeed before anyone has called `doAccept`
  (the unclaimed-call case)? That's the one behavior change that could
  break a working flow if gotten wrong.
- Plan 002 (TOCTOU on `doStartCall`) and plan 003 (`ac.bridge` data race)
  touch overlapping files (`httpapi.go`, `session.go`) — if executed in the
  same working tree as this plan, rebase/merge carefully; there is no
  logical dependency, just file overlap.
