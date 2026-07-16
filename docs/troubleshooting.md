# Troubleshooting WaCalls

**Files:** [`internal/app/webrtc.go`](../internal/app/webrtc.go), [`internal/voip/transport/sctprelay.go`](../internal/voip/transport/sctprelay.go), [`internal/app/auth.go`](../internal/app/auth.go)

Each entry is stated as symptom, then cause, then fix.

## WebRTC ICE fails, no audio

**Symptom:** the call connects and rings, the peer answers, but there is no audio
in either direction and the call drops itself after about 25 to 30 seconds. Server
logs show `browser ice state: checking` followed by `failed`.

**Cause:** in a container with more than one network interface (a Docker Swarm
overlay or a bridge plus the docker gateway), the media socket used to bind every
interface and send ICE connectivity checks from a private overlay source address
(for example `10.0.x.x`). No router accepts that source, so ICE never forms a
valid pair and the media watchdog ends the call.

**Fix:** already handled in the code. WaCalls binds the media socket to the
default-route interface only (read from `/proc/net/route`), so only the reachable
address is offered, and the 1:1 NAT rewrite advertises `WACALLS_PUBLIC_IP`. If you
still see this:

- Confirm `WACALLS_PUBLIC_IP` is set to the real public IP.
- Confirm the published UDP port matches `WEBRTC_UDP_PORT` exactly (`7881:7881/udp`).
- Confirm inbound UDP on that port is open in every firewall, including any cloud
  firewall in front of the host. Verify packets arrive:
  `tcpdump -i any -n udp port 7881`.

## Audio only flows one way

**Symptom:** in a clean browser audio works both ways, but in the browser you
normally use one direction is silent. The muted direction is the operator being
heard by the peer (the peer does not hear you; you still hear the peer).

**Cause:** a WhatsApp Web tab for a number involved in the call is logged in **in
the same browser** as WaCalls. When the call comes in, WhatsApp Web reacts and
grabs the microphone, so the WaCalls tab has no mic input. The muted direction is
exactly the one that needs the mic (operator to peer); playback (peer to operator)
still works because it does not use the mic. This is browser resource contention,
not a WaCalls or relay bug: a clean browser has the mic free and both directions
work.

**Fix:** run the WaCalls operator in a dedicated browser or browser profile with
no WhatsApp Web session for any number in the call. In real operator use (calling
external customers) this never happens, because the operator does not have the
customer's WhatsApp Web open.

Note: the relay port is not the cause here. WaCalls uses the port the WhatsApp
relay returns in each endpoint (`sctprelay.go`), falling back to `3480` only when
none is given; it does not hard-code `3478`.

## Login works but the session drops behind a proxy

**Symptom:** you sign in, but requests are treated as unauthenticated, or the
session cookie is not stored.

**Cause:** the session cookie is `Secure`, so it is only stored over HTTPS. Behind
a reverse proxy the app decides HTTPS from `X-Forwarded-Proto`. If the proxy does
not set it, the cookie is dropped.

**Fix:** make the proxy set `X-Forwarded-Proto: https` (Traefik does this by
default). Terminate TLS at the proxy and route to the app over the internal
network.

## Rate limit blocks everyone behind a proxy

**Symptom:** legitimate users get rate limited together, as if they shared one IP.

**Cause:** behind a proxy every request arrives from the proxy IP, so the per-IP
limit keys on that single address.

**Fix:** set `WACALLS_TRUSTED_PROXIES` to the proxy network (for example
`172.16.0.0/12` for docker networks). Then the limit keys by the real client from
`X-Forwarded-For` (rightmost untrusted hop). The Traefik compose sets this by
default.

## Pairing QR never scans or account will not connect

**Symptom:** the QR does not appear, or a paired account stays disconnected.

**Cause:** the QR is also printed in the container logs; a stale device or a wiped
data volume forces a re-pair.

**Fix:** read the logs (`docker compose logs -f wacalls`) for the QR. If an account
will not reconnect, remove it in the UI and pair again. Remember that removing the
data volume unpairs every account.
