<div align="center">

# 📞 WaCalls (Go)

**Native WhatsApp voice calls in pure Go, straight from the browser.**
Built for native VoIP media, multi-account (multi-session) operation, and a modern browser client.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)

[Overview](#overview) · [Architecture](#architecture) · [Quick Start](#quick-start) · [Docker](#docker) · [API](#api) · [Security](#security)

</div>

---

## Overview

WaCalls pairs one or more WhatsApp accounts via **QR code** and lets you **place and
receive 1:1 voice calls** from any browser on the LAN. The browser microphone is sent
as **raw 16 kHz PCM over a WebRTC data channel** to the Go server, which encodes it with
Meta's **MLow** codec and injects the media into WhatsApp's **SRTP relay** mesh — and the
reverse path brings the peer's audio back to the browser.

The entire VoIP stack runs **natively in pure Go**: the MLow voice codec, **RTP/SRTP**
packetization, **STUN**, the **WebRTC/SCTP relay** transport and the `<call>` signaling,
integrated with [**whatsmeow**](https://github.com/tulir/whatsmeow) and served to a
**React 19** client. There is **no cgo and no native DLL** — the MLow codec is a vendored
pure-Go package, so a plain `go build` produces a self-contained binary with live audio.
Peers whose clients lack MLow fall back to standard Opus on the receive path, decoded in
pure Go via [pion/opus](https://github.com/pion/opus).

Multiple WhatsApp accounts can be paired and operated side by side, each with its own
pairing QR, connection status, and history. A single account can also run **several
concurrent 1:1 calls** at once — one per browser operator — routed independently by call ID.

> **Status:** stable. Outgoing and incoming 1:1 calls reach `ACTIVE` with bidirectional
> audio, and a single account can hold several of them concurrently. Sessions persist in
> `wacalls.db` (pure-Go SQLite).

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          BROWSER (React client)                            │
│   mic + speaker  ·  WebRTC data channel (16 kHz PCM)  ·  HTTP + SSE         │
└───────────────────────────────┬──────────────────────────────────────────┘
                                 │  POST /api/sessions/{sid}/calls/{id}/webrtc  (SDP)
                                 │  GET  /api/events                            (SSE)
                                 ▼
┌──────────────────────────── GO SERVER (cmd/server) ────────────────────────┐
│  SessionManager   registry of accounts (client + CallManager + bridge)     │
│  Broker           SSE hub (sessions, auth, call lifecycle fan-out)          │
│  Bridge           pion WebRTC bridge (16 kHz PCM data channel ⇄ call core)  │
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
| `internal/voip/codec` | Audio codecs: vendored pure-Go MLow (`mlow/`) and the standard-Opus recv fallback (`opus/`) |
| `internal/voip/media` | RTP, SRTP, SSRC, PCM helpers, key derivation |
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
   ├── uplink   (you → peer): browser 16 kHz PCM (data channel) → MLow encode → SRTP → relay
   └── downlink (peer → you): relay → SRTP → MLow decode (or standard Opus for peers without MLow) → 16 kHz PCM (data channel) → browser

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

No C compiler, cgo, or native libraries are required — the MLow codec is vendored
pure Go (`internal/voip/codec/mlow`).

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

### Run

```bash
go run ./cmd/server -addr :8080          # add -debug for verbose logs
```

Live audio works out of the box — the MLow codec is pure Go, so a plain build
includes it. No build tags, no `CGO_ENABLED`, no DLLs.

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
| `-max-calls-per-session` | `8` | Max concurrent calls per session (`0` = unlimited) |

Calls that stay unanswered, unaccepted, or fail to establish media time out
automatically (60s ring/answer, 30s media connect), and active calls are capped at
4 hours. A timed-out call ends with reason `timeout` and releases its
`-max-calls-per-session` slot.

Ended calls are persisted to the session database (SQLite or PostgreSQL), so
`GET /api/sessions/{sid}/history` survives restarts; the table is capped at the
10,000 most recent records.

Set the `DATABASE_URL` environment variable to store everything on PostgreSQL instead of
SQLite (see [PostgreSQL backend](#postgresql-backend-optional)). Leaving it unset uses the
`-db` SQLite file.

---

## Docker

The server and the React client ship as a single self-contained image — a static
(`CGO_ENABLED=0`) Go binary plus the built `client/dist` on Alpine, ~30 MB. Images
are published to **[ghcr.io/jotadev66/wacalls](https://github.com/JotaDev66?tab=packages&repo_name=WaCalls)**.

### Run with Docker Compose

```bash
cp .env.example .env        # then set WACALLS_PUBLIC_IP
docker compose up -d        # or: make up
```

Open `http://<host>:8080`, click **New session**, and scan the QR (also printed in
`docker compose logs -f`). Sessions persist on the named volume `wacalls-data`.

### PostgreSQL backend (optional)

By default WaCalls stores everything - app sessions and whatsmeow device credentials - in a
single SQLite file (`-db`, persisted on the `wacalls-data` volume). To run everything on
PostgreSQL instead, set `DATABASE_URL`: a non-empty value selects Postgres, empty keeps SQLite.

Bring up the bundled Postgres with the compose overlay:

```bash
cp .env.example .env      # POSTGRES_* and DATABASE_URL are prefilled for the overlay
make up-postgres          # docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d
```

`DATABASE_URL` is a standard URL: `postgres://user:pass@host:5432/db?sslmode=disable` (keep
`sslmode=disable` for the bundled service, which has no TLS). Both the app's `sessions` table
and whatsmeow's `whatsmeow_*` tables live in that database. Switching an existing SQLite deploy
to Postgres starts with an empty database, so accounts must re-pair.

### WebRTC networking

The browser leg of a call runs over UDP, so the server must advertise an ICE host
candidate the browser can actually reach. Configure it in `.env`:

| Variable | Default | Meaning |
|---|---|---|
| `HTTP_PORT` | `8080` | Host port for the HTTP API + UI |
| `WEBRTC_UDP_PORT` | `7881` | Fixed UDP port all browser media is funneled through (single mux) |
| `WACALLS_PUBLIC_IP` | _(empty)_ | IP/host the browser uses to reach the server; published as the host candidate (1:1 NAT) |

On bridge networking set **both** `WACALLS_PUBLIC_IP` and `WEBRTC_UDP_PORT`. The
UDP port is published 1:1 (`7881:7881/udp`) so the advertised candidate matches the
reachable port — keep the two sides identical. Leaving the env vars unset falls back
to ephemeral ports with interface-IP candidates, which only works on host networking
or a flat LAN. The WhatsApp relay leg is outbound and needs no inbound ports.

### Build locally

```bash
docker compose build        # or: make build
docker build -t wacalls .   # plain build
```

Removing the volume (`make clean` / `docker compose down -v`) unpairs every account,
since `wacalls.db` holds the WhatsApp session credentials.

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

## Extending: telemetry and metering

WaCalls exposes two ports for observability and usage metering, both injected at the
composition root (`cmd/server/main.go`):

- `core.CallObserver`: one instance per call, created by the factory passed to
  `app.NewServer`. It receives phase marks (`core.MarkTransportSTUN`, `core.MarkTransportICE`,
  `core.MarkTransportDTLS`, `core.MarkTransportSCTPOpen`, `core.MarkMediaFirstPacket`),
  tracked allocations and goroutines, and exactly one `End(result, reason)` when the call
  finishes: `result` is `completed` when media connected, `failed` otherwise.
- `telemetry.CallTracer`: app-level call lifecycle with session, peer, direction and
  duration (`StartCall`, `MarkActive`, `EndCall`).

The built-in OpenTelemetry implementations are enabled by `OTEL_EXPORTER_OTLP_ENDPOINT`.
To stack a custom implementation (usage quotas, billing counters) next to them, combine
with `core.MultiObserver` and `telemetry.MultiTracer`:

```go
obsFactory := func(callID string) core.CallObserver {
	return core.MultiObserver(otelFactory(callID), meteringObserver(callID))
}
tracer := telemetry.MultiTracer(otelTracer, meteringTracer)
srv, err := app.NewServer(ctx, cfg, obsFactory, tracer, log)
```

---

## Security

Set `WACALLS_API_TOKEN` to require a bearer token on every `/api` and `/debug` route;
leave it empty to disable auth (trusted LAN only). Clients send it as
`Authorization: Bearer <token>`; the SSE stream (`/api/events`) takes it as a
`?access_token=<token>` query parameter. Set `WACALLS_CORS_ORIGINS` to a comma-separated
list of browser origins when the web client is served from a different origin than the
API; empty means same-origin only. Every `/api` route is rate limited per client IP
(default 20 requests per second, burst 40); tune it with `WACALLS_RATE_LIMIT` (requests
per second, `0` disables). Behind a reverse proxy all clients share the proxy address,
so the limit becomes effectively global; lower it or disable it accordingly.
`wacalls.db` holds WhatsApp session credentials
(secrets): **do not commit it** and keep it protected.

---

## Contributors

This project builds on the work of:

<div align="center">

<a href="https://github.com/jotadev66"><img src="https://github.com/jotadev66.png" width="72" height="72" style="border-radius:50%" alt="jotadev66"/></a>
<a href="https://github.com/jobasfernandes"><img src="https://github.com/jobasfernandes.png" width="72" height="72" style="border-radius:50%" alt="jobasfernandes"/></a>
<a href="https://github.com/edgardmessias"><img src="https://github.com/edgardmessias.png" width="72" height="72" style="border-radius:50%" alt="edgardmessias"/></a>
<a href="https://github.com/w3nder"><img src="https://github.com/w3nder.png" width="72" height="72" style="border-radius:50%" alt="w3nder"/></a>
<a href="https://github.com/purpshell"><img src="https://github.com/purpshell.png" width="72" height="72" style="border-radius:50%" alt="purpshell"/></a>

[**@jotadev66**](https://github.com/jotadev66) · [**@jobasfernandes**](https://github.com/jobasfernandes) · [**@edgardmessias**](https://github.com/edgardmessias) · [**@w3nder**](https://github.com/w3nder) · [**@purpshell**](https://github.com/purpshell)

</div>

---

## Acknowledgements

- [**whatsmeow**](https://github.com/tulir/whatsmeow) — Go WhatsApp Web protocol library
- [**pion/webrtc**](https://github.com/pion/webrtc) — pure-Go WebRTC stack (ICE + DTLS + SCTP)
- [**pion/opus**](https://github.com/pion/opus) — pure-Go Opus decoder (standard-Opus receive fallback)
- [**whatsapp-rust**](https://github.com/oxidezap/whatsapp-rust) — reference MLow codec implementation (ported to the vendored pure-Go `internal/voip/codec/mlow`)
- [**meowcaller**](https://github.com/purpshell/meowcaller) — WhatsApp VoIP calling engine reference
- [**zapo**](https://github.com/w3nder/zapo) — VoIP media-stack reference

---

## License

[MIT](./LICENSE)
