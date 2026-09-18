# Plan 005: Verify the SRTP/SRTCP auth tag on unprotect

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. This plan has a higher-than-usual chance of an
> unexpected real-world interaction (see "Why this matters" and STOP
> conditions) — read the whole plan before starting, don't just execute
> step by step blind. When done, update the status row for this plan in
> `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `internal/voip/media/srtp.go` is clean (no
> uncommitted changes) as of when this plan was written — check with
> `git diff --stat 3d2279e..HEAD -- internal/voip/media/srtp.go` and
> `git status --short -- internal/voip/media/srtp.go`. **But
> `internal/voip/media/srtcp.go` has UNCOMMITTED working-tree changes**
> (active branch `feature/video-nack-e-keyframe`) — do not rely on the
> commit diff for that file; run `git status --short -- internal/voip/media/srtcp.go`
> and compare the live file against the "Current state" excerpt below
> before proceeding. On any mismatch beyond what's already reflected in
> that excerpt, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED — see "Why this matters" and STOP conditions; this touches
  the active video-reverse-engineering branch's inbound RTCP path
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `3d2279e` + uncommitted working-tree changes to `internal/voip/media/srtcp.go`, 2026-09-17

## Why this matters

Both `SrtpContext.Unprotect` (audio + video RTP) and `SrtcpContext.Unprotect`
(RTCP: NACK, PLI/FIR, REMB, SR/RR) decrypt inbound packets but never verify
the HMAC auth tag that SRTP/SRTCP carries specifically to prove the
ciphertext wasn't forged or tampered with. `SrtcpContext.Unprotect`'s doc
comment even says so explicitly: "Não valida o tag de auth (o transporte já
é confiável); só descifra." AES-CTR is malleable — without a validated
MAC, anyone who can inject or bit-flip packets on the relay path (an
external, only semi-trusted WhatsApp relay) can corrupt or forge decrypted
content that then reaches `ParseNACK`/`IsPLIOrFIR`/`ParseREMB`/the H264
depacketizer as if legitimate, with no cryptographic integrity guarantee
despite the code deriving and holding exactly the key material needed to
provide one. Notably, `SrtpErrAuthFailed` is already declared as an error
type in `internal/voip/media/srtp.go` and never used anywhere — the
verification was evidently intended but never wired up.

**Read this before starting**: this branch (`feature/video-nack-e-keyframe`)
is mid-flight, actively reverse-engineering WhatsApp's non-standard "fast"
RTCP feedback channel — `internal/voip/media/srtcp.go`'s own comments
describe a real bug (a wrong IV bit-shift) that was only just fixed after
"por meses pareceu 'o WhatsApp usa outra chave'". This history means the
SRTCP auth key derivation (`srtcpLabelAuth = 0x04`) could *also* still be
wrong and simply never surfaced because nothing ever checked the tag. If
that's the case, turning on strict verification could start rejecting
100% of real inbound WhatsApp RTCP — which would look like "NACK/PLI/REMB
handling stopped working" rather than "a security fix landed". This plan
requires verifying against a real or previously-captured WhatsApp session
before it's considered done (see Step 5 and the STOP conditions) — do not
skip that verification and do not treat a passing unit-test suite alone as
sufficient.

## Current state

- `internal/voip/media/srtp.go` — SRTP (audio + video RTP) context; has a
  working `computeAuthTag` used only by `Protect`.
- `internal/voip/media/srtcp.go` — SRTCP (RTCP feedback) context; has a
  working `hmacSha1Trunc` used only by `Protect`, and an explicit
  "does not validate" comment on `Unprotect`.
- `internal/voip/call/callmanager_rtcp.go`, `callmanager_media.go`,
  `callmanager_video.go`, `videodump.go` — the four call sites of
  `Unprotect`/`UnprotectWithSSRC`; all already handle a returned error by
  logging and dropping the packet (no crash risk from a new error path).

`internal/voip/media/srtp.go:29-37,97-121,161-169` — relevant SRTP pieces
as they exist today:

```go
type SrtpContext struct {
	sessionKey  []byte
	sessionSalt []byte
	authKey     []byte
	roc         uint32
	lastSeq     uint16
	initialized bool
	authTagLen  int
}

func (c *SrtpContext) Unprotect(data []byte) (*RtpPacket, error) {
	if len(data) < 12 {
		return nil, &SrtpError{SrtpErrPacketTooShort, fmt.Sprintf("packet too short: %d bytes", len(data))}
	}

	header, err := DecodeRtpHeader(data)
	if err != nil {
		return nil, &SrtpError{SrtpErrDecryption, err.Error()}
	}
	headerSize := header.Size()
	payloadLen := len(data) - headerSize - c.authTagLen
	if payloadLen <= 0 {
		return nil, &SrtpError{SrtpErrPacketTooShort, fmt.Sprintf("no payload: %dB total, %dB header, auth=%d", len(data), headerSize, c.authTagLen)}
	}

	c.updateRoc(header.SequenceNumber)
	index := c.packetIndex(header.SequenceNumber)

	iv := c.generateIV(header.Ssrc, index)
	decrypted := make([]byte, payloadLen)
	if err := aesCtrXor(c.sessionKey, iv, data[headerSize:headerSize+payloadLen], decrypted); err != nil {
		return nil, &SrtpError{SrtpErrDecryption, err.Error()}
	}

	return &RtpPacket{Header: header, Payload: decrypted}, nil
}

func (c *SrtpContext) computeAuthTag(data []byte, roc uint32, tagLen int) []byte {
	mac := hmac.New(sha1.New, c.authKey)
	mac.Write(data)
	var rocBuf [4]byte
	binary.BigEndian.PutUint32(rocBuf[:], roc)
	mac.Write(rocBuf[:])
	sum := mac.Sum(nil)
	return sum[:tagLen]
}
```

Compare with `Protect` (unchanged by this plan, shown for reference — this
is the tag-computation shape `Unprotect` must mirror):

```go
if c.authTagLen > 0 {
	authData := output[:headerSize+len(packet.Payload)]
	tag := c.computeAuthTag(authData, c.roc, c.authTagLen)
	copy(output[headerSize+len(packet.Payload):], tag)
}
```

`SrtpErrAuthFailed` already exists, unused, in the error-type const block
near the top of `srtp.go`:

```go
const (
	SrtpErrPacketTooShort SrtpErrorType = "packet_too_short"
	SrtpErrAuthFailed     SrtpErrorType = "auth_failed"
	SrtpErrEncryption     SrtpErrorType = "encryption"
	SrtpErrDecryption     SrtpErrorType = "decryption"
)
```

`internal/voip/media/srtcp.go` — relevant SRTCP pieces as they exist in the
current (uncommitted) working tree:

```go
type SrtcpContext struct {
	sessionKey  []byte // 16
	sessionSalt []byte // 14
	authKey     []byte // 20
	authTagLen  int
	sendIndex   uint32
}

// Protect recebe um pacote RTCP composto em claro e devolve o SRTCP:
//
//	[8 bytes de header+SSRC em claro] [resto cifrado] [E(1)|SRTCP index(31)] [tag]
func (c *SrtcpContext) Protect(rtcp []byte) ([]byte, error) {
	if len(rtcp) < 8 {
		return nil, errors.New("srtcp: pacote RTCP curto demais")
	}
	c.sendIndex = (c.sendIndex + 1) & 0x7fffffff
	ssrc := binary.BigEndian.Uint32(rtcp[4:8])

	out := make([]byte, len(rtcp)+4+c.authTagLen)
	copy(out[:8], rtcp[:8])

	iv := c.srtcpIV(ssrc, c.sendIndex)
	if err := aesCtrXor(c.sessionKey, iv, rtcp[8:], out[8:len(rtcp)]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(out[len(rtcp):], c.sendIndex|0x80000000) // E=1

	tag := hmacSha1Trunc(c.authKey, out[:len(rtcp)+4], c.authTagLen)
	copy(out[len(rtcp)+4:], tag)
	return out, nil
}

// Unprotect descifra um SRTCP e devolve o pacote RTCP composto em claro. Não
// valida o tag de auth (o transporte já é confiável); só descifra.
func (c *SrtcpContext) Unprotect(data []byte) ([]byte, error) {
	return c.UnprotectWithSSRC(data, 0, false)
}

// UnprotectWithSSRC é igual a Unprotect, mas permite forçar o SSRC usado na
// derivação do IV em vez do "sender SSRC" que vem em claro no próprio pacote
// (useOverride=true). Existe pra diagnosticar empiricamente, via -video-dump,
// se o canal de feedback "fast" do WhatsApp deriva o IV de um SSRC diferente
// do que ele mesmo anuncia no cabeçalho (ex.: sempre "1" nos NACKs vistos).
func (c *SrtcpContext) UnprotectWithSSRC(data []byte, ssrcOverride uint32, useOverride bool) ([]byte, error) {
	if len(data) < 8+4+c.authTagLen {
		return nil, errors.New("srtcp: pacote curto demais")
	}
	body := data[:len(data)-c.authTagLen]
	eIdx := binary.BigEndian.Uint32(body[len(body)-4:])
	encrypted := eIdx&0x80000000 != 0
	index := eIdx & 0x7fffffff
	rtcpPart := body[:len(body)-4]
	if len(rtcpPart) < 8 {
		return nil, errors.New("srtcp: parte RTCP curta demais")
	}

	out := make([]byte, len(rtcpPart))
	copy(out[:8], rtcpPart[:8])
	if encrypted {
		ssrc := binary.BigEndian.Uint32(rtcpPart[4:8])
		if useOverride {
			ssrc = ssrcOverride
		}
		iv := c.srtcpIV(ssrc, index)
		if err := aesCtrXor(c.sessionKey, iv, rtcpPart[8:], out[8:]); err != nil {
			return nil, err
		}
	} else {
		copy(out[8:], rtcpPart[8:])
	}
	return out, nil
}

func hmacSha1Trunc(key, data []byte, n int) []byte {
	mac := hmac.New(sha1.New, key)
	mac.Write(data)
	sum := mac.Sum(nil)
	if n > len(sum) {
		n = len(sum)
	}
	return sum[:n]
}
```

**Critical design constraint — read before writing any code.** The auth
tag on the wire is computed over the *ciphertext as transmitted*
(`out[:len(rtcp)+4]` in `Protect`) — it does **not** depend on which SSRC
is used to derive the decryption IV. `UnprotectWithSSRC` is called from
`internal/voip/call/videodump.go`'s `debugTryNackSsrcCandidates`, which
deliberately tries **several different, mostly-wrong SSRC candidates** to
empirically figure out which one WhatsApp actually used for IV derivation
— that's an active reverse-engineering tool for this very branch. If tag
verification is added inside `UnprotectWithSSRC`, every candidate in that
loop would get the identical verification result (pass or fail) regardless
of which SSRC it's trying, because the tag check doesn't involve the SSRC
at all — but critically, if the *first* code path executed is "verify tag,
reject on mismatch, stop", the loop would never get to attempt decryption
with any candidate at all if the tag doesn't verify, breaking the tool.
**Therefore: add tag verification only to `Unprotect` (the production
entry point used by `handleInboundRtcp`), not to `UnprotectWithSSRC`.**
`UnprotectWithSSRC` stays exactly as it is — diagnostic-only, no
authentication, by design.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Vet | `go vet ./...` | exit 0 |
| Format check | `gofmt -l .` | empty output |
| Build | `go build ./...` | exit 0 |
| Test (this package) | `go test ./internal/voip/media/...` | all pass |
| Test (whole repo) | `go test ./...` | all pass |

## Scope

**In scope**:
- `internal/voip/media/srtp.go` (`Unprotect` only)
- `internal/voip/media/srtcp.go` (`Unprotect` only — **not** `UnprotectWithSSRC`)
- `internal/voip/media/foundation_test.go` (add SRTP auth-tag tests)
- `internal/voip/media/srtcp_test.go` (add SRTCP auth-tag tests)

**Out of scope**:
- `SrtcpContext.UnprotectWithSSRC` — must remain unauthenticated by design (see "Critical design constraint" above). Do not add verification here even though it looks like the "more thorough" place to put it.
- `internal/voip/call/videodump.go`'s `debugTryNackSsrcCandidates` — must keep calling `UnprotectWithSSRC` directly, unchanged.
- Plan 004 (gating `handleVideoNack` on SSRC) — separate, independent fix on the same general code path; do not merge the two.
- Any change to `srtcpIV`, `deriveSrtpKey`, or the KDF labels — not part of this fix.
- The call sites in `internal/voip/call/*.go` — they already handle a
  returned error correctly (log + drop); no change needed there unless a
  build error says otherwise.

## Git workflow

- Branch: `advisor/005-verify-srtp-srtcp-auth-tag` (create from
  `feature/video-nack-e-keyframe`).
- Commit message style: conventional commits, Portuguese scope/summary —
  e.g. `fix(media): valida tag de autenticação no unprotect de SRTP/SRTCP`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Verify the auth tag in `SrtpContext.Unprotect`

In `internal/voip/media/srtp.go`, insert tag verification right after
`c.updateRoc(header.SequenceNumber)` and before deriving the IV /
decrypting — mirror `Protect`'s tag computation exactly (same `authData`
shape: header + ciphertext, same `c.roc` after `updateRoc`):

```go
c.updateRoc(header.SequenceNumber)

if c.authTagLen > 0 {
	authData := data[:headerSize+payloadLen]
	wantTag := data[headerSize+payloadLen:]
	gotTag := c.computeAuthTag(authData, c.roc, c.authTagLen)
	if !hmac.Equal(gotTag, wantTag) {
		return nil, &SrtpError{SrtpErrAuthFailed, "auth tag mismatch"}
	}
}

index := c.packetIndex(header.SequenceNumber)
```

`hmac.Equal` is in the already-imported `crypto/hmac` package — no new
import needed.

**Verify**: `go build ./internal/voip/media/...` → exit 0.

### Step 2: Verify the auth tag in `SrtcpContext.Unprotect` only

In `internal/voip/media/srtcp.go`, add a small helper and use it from
`Unprotect` — leave `UnprotectWithSSRC` completely untouched:

```go
// Unprotect descifra um SRTCP e devolve o pacote RTCP composto em claro,
// validando o tag de autenticação primeiro (rejeita se não bater).
// UnprotectWithSSRC continua sem essa checagem — é ferramenta de
// diagnóstico (-video-dump) que testa SSRCs candidatos de propósito.
func (c *SrtcpContext) Unprotect(data []byte) ([]byte, error) {
	if err := c.verifyAuthTag(data); err != nil {
		return nil, err
	}
	return c.UnprotectWithSSRC(data, 0, false)
}

func (c *SrtcpContext) verifyAuthTag(data []byte) error {
	if len(data) < c.authTagLen {
		return errors.New("srtcp: pacote curto demais pro tag")
	}
	body := data[:len(data)-c.authTagLen]
	wantTag := data[len(data)-c.authTagLen:]
	gotTag := hmacSha1Trunc(c.authKey, body, c.authTagLen)
	if !hmac.Equal(gotTag, wantTag) {
		return errors.New("srtcp: auth tag inválido")
	}
	return nil
}
```

Update the doc comment on `Unprotect` to remove the now-false "não valida"
claim (shown inline above). Do not change `UnprotectWithSSRC`'s body or
its own doc comment.

**Verify**: `go build ./internal/voip/media/...` → exit 0; `gofmt -l internal/voip/media/srtp.go internal/voip/media/srtcp.go` → empty.

### Step 3: Confirm existing round-trip tests still pass

The existing `TestSrtpRoundtrip` (`internal/voip/media/foundation_test.go`)
and `TestSrtcpRoundTrip` (`internal/voip/media/srtcp_test.go`) protect then
unprotect with matching keys — they must still pass unchanged, since a
correctly-computed tag will verify successfully. If either fails, that
means the tag computation in Step 1/2 doesn't actually mirror `Protect`'s
— re-check the `authData`/`roc` inputs against `Protect` exactly.

**Verify**: `go test ./internal/voip/media/... -run 'TestSrtpRoundtrip|TestSrtcpRoundTrip' -v` → both pass.

### Step 4: Add tamper-rejection tests

In `internal/voip/media/foundation_test.go`, add a test near
`TestSrtpRoundtrip` that corrupts the tag after `Protect` and confirms
`Unprotect` rejects it:

```go
func TestSrtpUnprotectRejectsTamperedTag(t *testing.T) {
	callKey := bytes.Repeat([]byte{0x11}, 32)
	sendKM, _ := DerivePerJidSrtpKey(callKey, "self:0@lid")
	recvKM, _ := DerivePerJidSrtpKey(callKey, "peer:0@lid")

	sender, err := NewSrtpSession(sendKM, recvKM, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewSrtpSession(recvKM, sendKM, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}

	sess := NewWhatsAppOpusSession(0xAABBCCDD)
	payload := bytes.Repeat([]byte{0x42}, 40)
	pkt := sess.CreatePacketWithDuration(payload, 960, true)

	protected, err := sender.Protect(pkt)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), protected...)
	tampered[len(tampered)-1] ^= 0xFF // flip the last tag byte

	if _, err := receiver.Unprotect(tampered); err == nil {
		t.Fatal("Unprotect must reject a packet with a tampered auth tag")
	}

	// A tampered payload byte (tag untouched) must also be rejected.
	tamperedPayload := append([]byte(nil), protected...)
	tamperedPayload[pkt.Header.Size()] ^= 0xFF
	if _, err := receiver.Unprotect(tamperedPayload); err == nil {
		t.Fatal("Unprotect must reject a packet with a tampered payload")
	}
}
```

In `internal/voip/media/srtcp_test.go`, add the SRTCP equivalent, modeled
on the existing `TestSrtcpRoundTrip` in the same file:

```go
func TestSrtcpUnprotectRejectsTamperedTag(t *testing.T) {
	km, err := DerivePerJidSrtpKey(make([]byte, 32), "dev:0@s.whatsapp.net")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	send, err := NewSrtcpContext(km, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatalf("send ctx: %v", err)
	}
	recv, err := NewSrtcpContext(km, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatalf("recv ctx: %v", err)
	}

	compound := append(BuildSenderReport(0xAABBCCDD, 12345, 10, 4000),
		BuildREMB(0xAABBCCDD, 0x11223344, 900_000)...)

	enc, err := send.Protect(compound)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}

	tampered := append([]byte(nil), enc...)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := recv.Unprotect(tampered); err == nil {
		t.Fatal("Unprotect must reject a packet with a tampered auth tag")
	}

	// UnprotectWithSSRC is the diagnostic bypass and must NOT verify the
	// tag — it should still "succeed" (decrypt to garbage, no auth error)
	// even on the tampered packet, by design.
	if _, err := recv.UnprotectWithSSRC(tampered, 0, false); err != nil {
		t.Fatalf("UnprotectWithSSRC must not enforce auth (diagnostic bypass), got: %v", err)
	}
}
```

**Verify**: `go test ./internal/voip/media/... -run 'TestSrtpUnprotectRejectsTamperedTag|TestSrtcpUnprotectRejectsTamperedTag' -v` → both pass.

### Step 5: Manual/real-traffic verification (required — do not skip)

This step cannot be fully automated; do as much of it as your environment
allows and report clearly on what you could and couldn't verify:

- If a captured WhatsApp video-call packet trace is available in this repo
  or its `-video-dump` logs (check for any committed `.pcap`/log fixture —
  note `*.pcap`/`*.pcapng` are gitignored per `.gitignore`, so check
  outside git too if the operator has one locally), replay it through the
  new `SrtcpContext.Unprotect` and confirm real inbound RTCP (NACK/PLI/SR)
  verifies successfully, not just the synthetic round-trip test.
- If no captured trace is available, say so explicitly in your final
  report and flag that this fix has only been verified against
  synthetically-generated (self-consistent) packets, not real WhatsApp
  traffic — this is a meaningful caveat given the "Why this matters"
  history of a previously-undetected SRTCP bug on this exact code path.

## Test plan

- `internal/voip/media/foundation_test.go`: `TestSrtpUnprotectRejectsTamperedTag` (Step 4).
- `internal/voip/media/srtcp_test.go`: `TestSrtcpUnprotectRejectsTamperedTag` (Step 4), including the `UnprotectWithSSRC` bypass assertion.
- Re-run `TestSrtpRoundtrip` and `TestSrtcpRoundTrip` to confirm no regression (Step 3).
- Full suite: `go test ./...` → all pass.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `gofmt -l .` is empty
- [ ] `go test ./...` exits 0, including all four tests named above
- [ ] `SrtpContext.Unprotect` returns `SrtpErrAuthFailed` on a tampered tag or payload
- [ ] `SrtcpContext.Unprotect` returns an error on a tampered tag; `SrtcpContext.UnprotectWithSSRC` does not (bypass preserved)
- [ ] Step 5's manual verification was attempted and its outcome (verified against real traffic / only synthetic) is reported
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row for 005 updated

## STOP conditions

- The code at `internal/voip/media/srtcp.go` doesn't match the "Current
  state" excerpt (the working tree has drifted further since this plan was
  written) — re-read and compare before proceeding.
- `TestSrtpRoundtrip` or `TestSrtcpRoundTrip` (the existing round-trip
  tests) start failing after your change — this means the tag computation
  doesn't mirror `Protect`, not that the tests are wrong. Do not weaken or
  delete these tests to make them pass.
- Step 5's real-traffic verification shows the auth check rejecting
  legitimate WhatsApp RTCP — **do not** respond by silently removing or
  weakening the check, and do not guess at a key-derivation fix. STOP and
  report: this would mean the SRTCP auth key derivation itself
  (`srtcpLabelAuth`) is suspect, which is a bigger, separate investigation
  than this plan's scope.
- A step's verification fails twice after a reasonable fix attempt.
- You find a caller of `Unprotect` (SRTP or SRTCP) that does *not* already
  handle a returned error gracefully (grep `.Unprotect(` across
  `internal/voip/call/*.go` to confirm all four call sites still just log
  and return/continue) — if one doesn't, STOP and report rather than
  changing its error-handling behavior as a side effect of this plan.

## Maintenance notes

- `SrtcpContext.UnprotectWithSSRC` is now the **only** unauthenticated
  decrypt path in this codebase, and that's intentional — any reviewer
  extending its use beyond `debugTryNackSsrcCandidates` (e.g. wiring it
  into a new production code path) must understand it bypasses
  authentication and should not be used for anything except offline/manual
  SSRC reverse-engineering.
- If the SRTCP "fast" RTCP format reverse-engineering (ongoing on this
  branch) ever determines the auth key derivation needs to change, this
  plan's `verifyAuthTag` is the one place that assumption is encoded —
  update it there, not by reintroducing an unauthenticated path.
- This plan and plan 004 both touch the inbound-RTCP-trust surface
  (`handleInboundRtcp` and what it calls) — independent fixes, but a
  reviewer assessing "is inbound RTCP now trustworthy" should look at both.
