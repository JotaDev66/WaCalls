# Plan 004: Gate handleVideoNack on the NACK's mediaSSRC

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: This repo's working tree has UNCOMMITTED
> changes to `internal/voip/call/callmanager_rtcp.go` as of when this plan
> was written (active branch `feature/video-nack-e-keyframe`, base commit
> `3d2279e`). Do **not** rely on `git diff 3d2279e..HEAD` alone — also run
> `git status --short -- internal/voip/call/callmanager_rtcp.go` and, if it
> shows changes beyond what's already reflected in the "Current state"
> excerpt below, re-read the live file and compare line-by-line before
> proceeding. On a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `3d2279e` + uncommitted working-tree changes, 2026-09-17

## Why this matters

`handleVideoNack(mediaSSRC uint32, seqs []uint16, selfV uint32)` takes both
`mediaSSRC` (which media stream the inbound NACK is naming) and `selfV`
(our own video SSRC) as parameters — but the function body only uses them
in the debug log line at the end. The retransmission loop runs
unconditionally for every sequence number in the NACK, regardless of
whether `mediaSSRC` actually names our video stream. Any RTPFB/NACK
sub-packet found while parsing an inbound compound RTCP packet
(`handleInboundRtcp`, which calls `handleVideoNack`) triggers a resend,
even if it's naming some other stream. Combined with the fact that SRTCP
`Unprotect` doesn't yet verify the packet's authenticity either (a
separate, larger fix — plan 005), the retransmission path currently has no
correctness gate independent of the crypto layer: a single inbound
compound packet with several stacked NACK FCI blocks could trigger a
disproportionate number of `hist.get`+`relay.Broadcast` calls. The fix is
small: the parameter `selfV` is already threaded through for exactly this
purpose — it's just never checked.

## Current state

- `internal/voip/call/callmanager_rtcp.go` — `handleVideoNack` and its
  caller `handleInboundRtcp`.

`internal/voip/call/callmanager_rtcp.go` (current `handleVideoNack`, as it
exists in the working tree right now):

```go
// handleVideoNack retransmite os pacotes de vídeo que o peer pediu num RTCP NACK,
// relendo-os do histórico já protegido (SRTP) e reenviando pelo relay sem tocar
// no estado do payloader. Chamada sem m.mu travado.
func (m *CallManager) handleVideoNack(mediaSSRC uint32, seqs []uint16, selfV uint32) {
	m.mu.Lock()
	hist := m.videoTxHist
	m.mu.Unlock()
	if hist == nil || len(seqs) == 0 {
		return
	}

	var resent, missing int
	for _, seq := range seqs {
		pkt, ok := hist.get(seq)
		if !ok {
			missing++
			continue
		}
		m.relay.Broadcast(pkt)
		resent++
	}

	m.mu.Lock()
	m.videoRtxResent += uint32(resent)
	total := m.videoRtxResent
	m.mu.Unlock()

	if VideoDump {
		m.log.Info("VDUMP rx-rtcp NACK", "media_ssrc", mediaSSRC, "self_video_ssrc", selfV,
			"pedidos", len(seqs), "reenviados", resent, "fora_do_buffer", missing, "reenviados_total", total)
	}
}
```

The call site, in `handleInboundRtcp` (same file, inside the
`media.SplitRTCPCompound(plain)` loop):

```go
if mediaSSRC, seqs, ok := media.ParseNACK(sub); ok {
	if VideoDump {
		...
	}
	m.handleVideoNack(mediaSSRC, seqs, selfV)
	continue
}
```

where `selfV := m.selfVideoSsrc` was captured earlier in the same function
under `m.mu.Lock()`.

**Edge case to be aware of, not to solve differently**: if `selfV` is `0`
(our video SSRC not yet assigned — e.g. a NACK arrives before we've sent
any video), a NACK whose `mediaSSRC` also happens to be `0` would pass an
equality gate. This is an unlikely wire condition (WhatsApp's real NACKs
name a real SSRC) and not worth special-casing — a straightforward
`mediaSSRC != selfV` gate matches what the existing parameter naming
already implies was intended. Do not add extra handling for the `0 == 0`
case beyond the plain equality check.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Vet | `go vet ./...` | exit 0 |
| Format check | `gofmt -l .` | empty output |
| Build | `go build ./...` | exit 0 |
| Test (this package) | `go test ./internal/voip/call/...` | all pass |
| Test (whole repo) | `go test ./...` | all pass |

## Scope

**In scope**:
- `internal/voip/call/callmanager_rtcp.go` (`handleVideoNack` only)
- `internal/voip/call/callmanager_video_test.go` (extend `TestVideoNackRetransmit` or add a new test function)

**Out of scope**:
- `handleInboundRtcp` itself — its call site (`m.handleVideoNack(mediaSSRC, seqs, selfV)`) does not need to change, the gate belongs inside `handleVideoNack`.
- SRTCP auth-tag verification — that's plan 005, a separate and larger fix; do not attempt it here.
- Any other branch inside `handleInboundRtcp` (PLI/FIR handling, REMB handling) — untouched by this plan.
- The NACK-parsing logic in `internal/voip/media` (`ParseNACK`) — not part of this fix.

## Git workflow

- Branch: `advisor/004-gate-video-nack-on-ssrc` (create from
  `feature/video-nack-e-keyframe` — this plan's file is already mid-edit on
  that branch, so branch from the current working tree state, not from a
  clean checkout of `3d2279e`).
- Commit message style: conventional commits, Portuguese scope/summary —
  e.g. `fix(video): ignora NACK que não é do nosso SSRC de vídeo`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add the SSRC gate

In `internal/voip/call/callmanager_rtcp.go`, add an early return at the top
of `handleVideoNack`, right after the existing `hist == nil ||
len(seqs) == 0` check:

```go
func (m *CallManager) handleVideoNack(mediaSSRC uint32, seqs []uint16, selfV uint32) {
	m.mu.Lock()
	hist := m.videoTxHist
	m.mu.Unlock()
	if hist == nil || len(seqs) == 0 {
		return
	}
	if mediaSSRC != selfV {
		if VideoDump {
			m.log.Info("VDUMP rx-rtcp NACK ignorado (SSRC não é nosso vídeo)",
				"media_ssrc", mediaSSRC, "self_video_ssrc", selfV)
		}
		return
	}

	var resent, missing int
	// ... rest unchanged
```

Keep everything below unchanged (the retransmit loop, the
`videoRtxResent` counter update, the existing `VideoDump` log line at the
end).

**Verify**: `go build ./internal/voip/call/...` → exit 0; `gofmt -l internal/voip/call/callmanager_rtcp.go` → empty.

### Step 2: Extend the existing NACK test

`TestVideoNackRetransmit` in `internal/voip/call/callmanager_video_test.go`
already calls `m.handleVideoNack(m.selfVideoSsrc, ..., m.selfVideoSsrc)` —
i.e. matching SSRC — so it will keep passing unchanged. Add a new case at
the end of that same test function (reuse its existing `m`, `fr`, `first`
variables — do not re-derive SRTP/video setup) asserting that a mismatched
`mediaSSRC` sends nothing:

```go
	// Um NACK que não é do nosso SSRC de vídeo não deve reenviar nada.
	sentBefore = len(fr.sent)
	m.handleVideoNack(m.selfVideoSsrc+1, []uint16{seqOf(first)}, m.selfVideoSsrc)
	if len(fr.sent) != sentBefore {
		t.Error("NACK com SSRC diferente do nosso não devia reenviar nada")
	}
```

Insert this right before the test function's closing `}`, after the
existing "seq desconhecido não devia reenviar nada" block.

**Verify**: `go test ./internal/voip/call/... -run TestVideoNackRetransmit -v` → pass, including the new assertion.

## Test plan

- Extend `TestVideoNackRetransmit` in
  `internal/voip/call/callmanager_video_test.go` with the mismatched-SSRC
  case from Step 2 — this is the direct regression test for this fix.
- Confirm the two existing cases in the same test (matching-SSRC resend,
  unknown-seq no-op) still pass unchanged.
- Full suite: `go test ./...` → all pass.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `gofmt -l .` is empty
- [ ] `go test ./...` exits 0, including the extended `TestVideoNackRetransmit`
- [ ] `handleVideoNack` returns early when `mediaSSRC != selfV`, before touching `m.videoTxHist`/`relay.Broadcast`
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row for 004 updated

## STOP conditions

- The code at `internal/voip/call/callmanager_rtcp.go` doesn't match the
  "Current state" excerpt (the working tree has drifted further since this
  plan was written) — re-read and compare before proceeding.
- A step's verification fails twice after a reasonable fix attempt.
- You find `handleVideoNack` is called from anywhere other than
  `handleInboundRtcp` (grep `handleVideoNack(` across the repo to confirm)
  — if so, STOP and report the additional call site instead of assuming
  the gate is safe for it too.
- `TestVideoNackRetransmit`'s existing (pre-this-plan) assertions start
  failing after your change — that means the gate is rejecting the
  matching-SSRC case too, which means the equality check itself is wrong;
  STOP and report rather than loosening the gate to make the test pass.

## Maintenance notes

- This plan and plan 005 (SRTCP auth-tag verification) both hunt bugs
  reachable via crafted/corrupted inbound RTCP on the same code path
  (`handleInboundRtcp`) — they're independent fixes (this one is a logic
  gate, plan 005 is cryptographic authentication) and can land in either
  order, but a reviewer evaluating "is inbound RTCP now safe to trust"
  should look at both together.
- If a future change adds handling for *audio* NACKs (currently this
  function and the surrounding code only handle video), the same
  SSRC-gating principle should apply there too — check against
  `m.selfSsrc` (audio), not `m.selfVideoSsrc`.
