<div align="center">

# 📞 WaCalls (Go)

**Native WhatsApp voice calls in pure Go, straight from the browser.**
Built for native VoIP media, multi-account (multi-session) operation, and a modern browser client.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)

[Overview](#overview) · [Architecture](#architecture) · [Quick Start](#quick-start) · [API](#api) · [Security](#security)

</div>

---

## Overview

WaCalls pairs one or more WhatsApp accounts via **QR code** and lets you **place and
receive 1:1 voice calls** from any browser on the LAN. The browser microphone is sent
over **WebRTC (Opus)** to the Go server, which transcodes to Meta's **MLow** codec and
injects the media into WhatsApp's **SRTP relay** mesh — and the reverse path brings the
peer's audio back to the browser.

The entire VoIP stack runs **natively in Go**: the MLow voice codec, **RTP/SRTP**
packetization, **STUN**, the **WebRTC/SCTP relay** transport and the `<call>` signaling,
integrated with [**whatsmeow**](https://github.com/tulir/whatsmeow) and served to a
**React 19** client. The only C dependency is the `opus_mlow` codec (via cgo), and even
that is optional — without it the server runs in **signaling-only** mode (pairing and
call setup work; no live audio).

Multiple WhatsApp accounts can be paired and operated side by side, each with its own
pairing QR, connection status, call manager, and history.

> **Status:** stable. Outgoing and incoming 1:1 calls reach `ACTIVE` with bidirectional
> audio. Sessions persist in `wacalls.db` (pure-Go SQLite).

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          BROWSER (React client)                            │
│   mic + speaker  ·  WebRTC (Opus 48 kHz)  ·  HTTP + SSE                     │
└───────────────────────────────┬──────────────────────────────────────────┘
                                 │  POST /api/sessions/{sid}/calls/{id}/webrtc  (SDP)
                                 │  GET  /api/events                            (SSE)
                                 ▼
┌──────────────────────────── GO SERVER (cmd/server) ────────────────────────┐
│  SessionManager   registry of accounts (client + CallManager + bridge)     │
│  Broker           SSE hub (sessions, auth, call lifecycle fan-out)          │
│  Bridge           pion WebRTC bridge (browser Opus ⇄ 16 kHz PCM)            │
│                                                                            │
│  internal/wa      VoipSocket adapter over whatsmeow                        │
│  internal/voip    call · signaling · media · transport · core · wanode     │
└───────────────┬──────────────────────────────────────┬────────────────────┘
                │ <call> signaling (Signal/USync)       │ SRTP media
                ▼                                        ▼
        ┌───────────────┐                    ┌──────────────────────┐
        │  WhatsApp WS  │                    │   WhatsApp relay      │
        │  (whatsmeow)  │                    │  (SRTP over SCTP/DC)  │
        └───────────────┘                    └──────────────────────┘
```

### Layout

| Path | Responsibility |
|---|---|
| `cmd/server` | HTTP/SSE broker, session manager + store, WebRTC bridge, process lifecycle |
| `internal/wa` | `VoipSocket` — sends/receives `<call>` stanzas via whatsmeow |
| `internal/voip/core` | Domain types, constants, the `VoipSocket` interface |
| `internal/voip/wanode` | Shared WhatsApp-node and JID helpers |
| `internal/voip/media` | MLow codec, RTP, SRTP, SSRC, resampling, key derivation |
| `internal/voip/transport` | SCTP relay, STUN, subscription encoding |
| `internal/voip/signaling` | `<call>` stanza build/parse, call-key crypto, relay-ack parsing |
| `internal/voip/call` | `CallManager` — orchestrates a single call end to end |
| `client/` | React 19 + Vite + Tailwind v4 + shadcn/ui (dialer, call cards, sessions, history) |

---

## How a call flows

The core is `internal/voip/call.CallManager`, which drives a call end to end. Outgoing
call sequence:

```
1. POST .../calls            → CallManager.StartCall(peerJid)
                               generates a callID, builds the <call> offer, sends it

2. Browser opens WebRTC      → POST .../calls/{id}/webrtc (SDP offer)
                               the bridge answers with an SDP answer (pion)

3. Peer accepts              → events.CallAccept → HandleCallAccept
                               server receives <relay> + hop-by-hop keys

4. Relay transport           → STUN binding/allocate on WhatsApp relays
                               ICE + DTLS + SCTP DataChannel connect (pion)

5. SRTP media flowing        → state goes ACTIVE
   ├── uplink   (you → peer): browser Opus → 48→16 kHz → MLow encode → SRTP → relay
   └── downlink (peer → you): relay → SRTP → MLow decode → 16→48 kHz → Opus → browser

6. Teardown                  → DELETE .../calls/{id} or events.CallTerminate
                               CallManager.EndCall + bridge cleanup
```

Each protocol step (hop-by-hop SRTP key derivation, RTP packetization at `PT=120`/16 kHz,
STUN relay registration, relay-ack and `<call>` stanza parsing) is implemented and covered
by tests in `internal/voip` (`go test ./...`).

---

## Requirements

- **Go 1.26+**
- **Node 22+** and **npm** (only to build/run the React client)
- **Live audio (optional):** a C compiler and the native MLow codec library.
  - **Windows:** **MSYS2 + mingw-w64-gcc** at `C:\msys64\mingw64\bin`, plus the bundled
    `native/opus_mlow.dll` + `native/libopus-0.dll`.
  - **macOS / Linux:** build `native/libopus_mlow.a` once with `scripts/build-mlow.sh`
    (see *Run (live audio)* below). It is git-ignored and architecture-specific, so it is
    not committed.

---

## Quick Start

```bash
# clone and enter the project
git clone <repo-url> wacalls-go
cd wacalls-go

# Go dependencies
go mod download

# React client dependencies
cd client && npm install && cd ..
```

### Run (signaling-only — no C compiler; pairs and calls, no audio)

```bash
go run ./cmd/server -addr :8080          # add -debug for verbose logs
```

### Run (live audio — native MLow codec via cgo)

**Windows (recommended):**

```powershell
.\run-mlow.ps1                            # builds with -tags mlow + cgo and starts
.\run-mlow.ps1 -addr :9090               # extra flags are forwarded
```

**Manual (any platform with a C toolchain):**

```powershell
$env:PATH = "C:\msys64\mingw64\bin;$PWD\native;$env:PATH"
$env:CGO_ENABLED = "1"; $env:CC = "gcc"
go run -tags mlow ./cmd/server -addr :8080 -debug
```

**macOS / Linux:**

The native MLow codec is built from
[`edgardmessias/opus_mlow`](https://github.com/edgardmessias/opus_mlow) into the static
library `native/libopus_mlow.a` (the cgo build links it via `-lopus_mlow`). It is **not
committed** — it is architecture-specific and would clash with the Windows `.dll` at link
time — so build it once, then run:

```bash
# build native/libopus_mlow.a (needs cmake + a C toolchain); git-ignored
scripts/build-mlow.sh

# start with the native codec
CGO_ENABLED=1 go run -tags mlow ./cmd/server -addr :8080 -debug
```

`scripts/build-mlow.sh` clones and builds opus_mlow and copies the static library into
`native/`. To do it by hand:

```bash
git clone https://github.com/edgardmessias/opus_mlow.git
cmake -S opus_mlow -B opus_mlow/build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
cmake --build opus_mlow/build
cp opus_mlow/build/libopus.a native/libopus_mlow.a
```

> `native/libopus_mlow.a` is git-ignored and architecture-specific — build it on the
> machine you run on (the linker would otherwise pick a mismatched library over the
> Windows `.dll`). Without it (or without `-tags mlow`) the server runs in
> **signaling-only** mode (pairing and call setup work; no audio).

Open `http://localhost:8080`, click **New session**, and scan the QR shown in the browser
(it is also printed in the terminal) with **WhatsApp → Linked devices**. Add more accounts
the same way and switch between them in the sidebar.

### React client in dev mode

```bash
cd client
npm run dev      # Vite on :5173, proxies /api → http://localhost:8080
```

For production, build the static client and serve it from the Go server:

```bash
cd client && npm run build && cd ..
go run ./cmd/server -static client/dist -addr :8080
```

### Server flags

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | HTTP listen address |
| `-db` | `wacalls.db` | SQLite session database path |
| `-static` | `client/dist` | Static client directory (optional) |
| `-debug` | `false` | Verbose logging (includes whatsmeow's internal log) |

---

## API

All routes are session-scoped. Events stream over a single SSE channel, tagged with the
originating `sessionId`.

| Method | Route | Purpose |
|---|---|---|
| `GET` | `/api/sessions` | List accounts (id, name, jid, status, paired) |
| `POST` | `/api/sessions` | Create an account and begin QR pairing |
| `DELETE` | `/api/sessions/{sid}` | Log out and remove an account |
| `POST` | `/api/sessions/{sid}/logout` | Disconnect an account (keep it for re-pairing) |
| `POST` | `/api/sessions/{sid}/pair` | Re-pair an account (emit a fresh QR) |
| `POST` | `/api/sessions/{sid}/calls` | Start an outgoing call (`{ phone, duration_ms?, record? }`) |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc` | Exchange the browser WebRTC SDP |
| `POST` | `/api/sessions/{sid}/calls/{id}/accept` | Accept an incoming call |
| `POST` | `/api/sessions/{sid}/calls/{id}/reject` | Reject an incoming call |
| `DELETE` | `/api/sessions/{sid}/calls/{id}` | End an active call |
| `GET` | `/api/sessions/{sid}/history` | Recent call history (up to 50 records) |
| `GET` | `/api/events` | Server-sent events (sessions, auth, call lifecycle) |

---

## Tests

```bash
go test ./...                 # media stack: SRTP, STUN, RTP, relay-ack, codec, state
cd client && npm run build    # client type-check + production build
```

---

## Security

The API has **no authentication** — anyone with HTTP access can create accounts, place
calls, and read history. **Run it only on a trusted LAN.** `wacalls.db` holds WhatsApp
session credentials (secrets): **do not commit it** and keep it protected.

---

## Contributors

This project builds on the work of:

<div align="center">

<a href="https://github.com/jotadev66"><img src="https://github.com/jotadev66.png" width="72" height="72" style="border-radius:50%" alt="jotadev66"/></a>
<a href="https://github.com/jobasfernandes"><img src="https://github.com/jobasfernandes.png" width="72" height="72" style="border-radius:50%" alt="jobasfernandes"/></a>
<a href="https://github.com/edgardmessias"><img src="https://github.com/edgardmessias.png" width="72" height="72" style="border-radius:50%" alt="edgardmessias"/></a>
<a href="https://github.com/w3nder"><img src="https://github.com/w3nder.png" width="72" height="72" style="border-radius:50%" alt="w3nder"/></a>

[**@jotadev66**](https://github.com/jotadev66) · [**@jobasfernandes**](https://github.com/jobasfernandes) · [**@edgardmessias**](https://github.com/edgardmessias) · [**@w3nder**](https://github.com/w3nder)

</div>

---

## Acknowledgements

- [**whatsmeow**](https://github.com/tulir/whatsmeow) — Go WhatsApp Web protocol library
- [**pion/webrtc**](https://github.com/pion/webrtc) — pure-Go WebRTC stack (ICE + DTLS + SCTP)
- [**opus_mlow**](https://github.com/edgardmessias/opus_mlow) — native MLow codec
- [**zapo**](https://github.com/w3nder/zapo) — VoIP media-stack reference

---

## License

[MIT](./LICENSE)
