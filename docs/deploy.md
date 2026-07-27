# Deploying WaCalls

**Files:** [`docker-compose.yml`](../docker-compose.yml), [`docker-compose.postgres.yml`](../docker-compose.postgres.yml), [`docker-compose.traefik.yml`](../docker-compose.traefik.yml), [`.env.example`](../.env.example), [`Dockerfile`](../Dockerfile)

This guide takes you from zero to a WaCalls instance that can pair a WhatsApp
account and place calls from the browser. If audio or pairing misbehaves, see
[troubleshooting.md](./troubleshooting.md).

## Overview

WaCalls ships as one self-contained image (a static Go binary plus the built
React client, about 30 MB). A call has two network legs:

- **HTTP/UI leg** (TCP): the browser talks to the server over HTTP. Put this
  behind a reverse proxy for TLS.
- **Media leg** (UDP): browser audio flows over WebRTC to a single fixed UDP
  port. This must be reachable directly (it does not go through the proxy).
- The WhatsApp relay leg is outbound only and needs no inbound ports.

## Requirements

- A host with a **public IP** (or 1:1 NAT to one) and Docker + Docker Compose.
- Inbound **TCP 80/443** (HTTP/TLS) and inbound **UDP 7881** (WebRTC media) open
  in every firewall in front of the host, including any cloud firewall.
- For the TLS option: a **domain name** whose A record points to the host.

## Choose a deployment

| Option | TLS | Use when |
|---|---|---|
| A. Compose (plain HTTP) | no | LAN or a quick test; browser mic needs HTTPS, so this is dev only |
| B. Compose + Traefik | yes (auto) | A single-host production deploy with a domain |
| C. Docker Swarm | yes (existing) | You already run a Traefik/Swarm stack |

### Option A: Compose, plain HTTP (dev/LAN)

```bash
cp .env.example .env
# set WACALLS_PUBLIC_IP to the host IP, and WACALLS_ADMIN_USER / WACALLS_ADMIN_PASSWORD
docker compose up -d            # or: make up
# Postgres instead of SQLite:
make up-postgres
```

Open `http://<host>:8080`. Browsers only grant the microphone on a secure
context (`https://` or `localhost`), so plain HTTP over a public IP cannot
capture the operator's mic. Use this for wiring checks, not real calls.

### Option B: Compose + Traefik (recommended, single host)

This brings up Traefik (with automatic Let's Encrypt TLS), WaCalls, and Postgres
together. It is self-contained: no other reverse proxy required.

```bash
cp .env.example .env
```

Set at least these in `.env`:

```dotenv
WACALLS_DOMAIN=calls.example.com     # A record points here
ACME_EMAIL=you@example.com           # Let's Encrypt account email
WACALLS_PUBLIC_IP=203.0.113.10       # the host public IP
WACALLS_ADMIN_USER=admin
WACALLS_ADMIN_PASSWORD=<strong password>
# optional automation token (Authorization: Bearer <token>):
WACALLS_API_TOKEN=
```

Bring it up:

```bash
docker compose -f docker-compose.traefik.yml up -d --build
```

Traefik serves `https://$WACALLS_DOMAIN` (HTTP redirects to HTTPS) and publishes
UDP 7881 straight to the container for media. Postgres holds the app sessions and
the WhatsApp device credentials on the `wacalls-pg` volume.

### Option C: Docker Swarm (existing Traefik)

If you already run a Traefik/Swarm stack, deploy WaCalls as a stack whose HTTP
router is discovered by your Traefik, and publish UDP 7881 in `host` mode so ICE
sees the real client:

```yaml
services:
  wacalls:
    image: ghcr.io/jotadev66/wacalls:latest
    deploy:
      labels:
        - "traefik.enable=true"
        - "traefik.docker.network=<your-public-overlay>"
        - "traefik.http.routers.wacalls.rule=Host(`calls.example.com`)"
        - "traefik.http.routers.wacalls.entrypoints=websecure"
        - "traefik.http.routers.wacalls.tls.certresolver=<your-resolver>"
        - "traefik.http.services.wacalls.loadbalancer.server.port=8080"
    environment:
      DATABASE_URL: "postgres://wacalls:<pass>@postgres:5432/wacalls?sslmode=disable"
      WACALLS_ADMIN_USER: admin
      WACALLS_ADMIN_PASSWORD: <strong password>
      WACALLS_PUBLIC_IP: 203.0.113.10
      WACALLS_WEBRTC_UDP_PORT: "7881"
    ports:
      - target: 7881
        published: 7881
        protocol: udp
        mode: host
    networks:
      - <your-public-overlay>
      - wacalls-internal
```

On Swarm the media container sits behind the overlay network. WaCalls handles the
multi-interface case automatically (see WebRTC networking below), so no extra
config is needed beyond `WACALLS_PUBLIC_IP` and the `host`-mode UDP port.
Host-mode publishing already forwards inbound UDP with iptables DNAT that
preserves the real client IP, so no Docker daemon changes are required.

## WebRTC networking

The server advertises one ICE host candidate that the browser dials over UDP.

| Variable | Default | Meaning |
|---|---|---|
| `WACALLS_PUBLIC_IP` | _(empty)_ | Public IP/host the browser reaches; advertised as the host candidate (1:1 NAT) |
| `WEBRTC_UDP_PORT` | `7881` | Fixed UDP port all browser media funnels through |

Set `WACALLS_PUBLIC_IP` on any non-LAN deploy. Inside a container the server binds
the media socket to the **default-route interface only**, so private
overlay/bridge addresses are never offered as candidates. That makes the same
image work on plain bridge, on a Swarm overlay, and on bare metal without
per-environment code changes; only `WACALLS_PUBLIC_IP` varies. If ICE still fails,
see [troubleshooting.md](./troubleshooting.md#webrtc-ice-fails-no-audio).

## Verify before going live

Run the built-in preflight (binds the UDP port, checks the public IP, discovers the
external IP via STUN and cross-checks it against `WACALLS_PUBLIC_IP`, opens the
database):

```bash
docker compose exec wacalls wacalls -doctor
```

## Pair an account

1. Open the UI, sign in with the admin credentials.
2. Click **New session** and scan the QR (also printed in the container logs).
3. Change the admin password in the UI. The hash lives in the database and
   survives restarts.

## Notes

- Image tags: `ghcr.io/jotadev66/wacalls:latest` is production, `:develop` is the
  beta channel. Pick one with `WACALLS_IMAGE` in `.env`.
- Codec variants: the default tags ship the native MLow encoder (`nativemlow`
  build tag, ~5x faster encode for high call density); the `-pure` suffixed tags
  (`:latest-pure`, `:develop-pure`) are the `CGO_ENABLED=0` pure-Go build. Both
  decode with the same pure-Go decoder. Self-builders choose with
  `docker build --build-arg VARIANT=native .` (default `pure` = pure Go).
- Removing the data volume (`docker compose down -v`) unpairs every account.
- Switching an existing SQLite deploy to Postgres starts empty; accounts re-pair.
- Use a dedicated browser or profile for the WaCalls operator: a WhatsApp Web tab
  for a number involved in the call, in the same browser, can grab the microphone.
  See [troubleshooting.md](./troubleshooting.md#audio-only-flows-one-way).
