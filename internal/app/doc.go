// Package app wires the HTTP/SSE front end to the session workers and the
// event broker that couples them. Files group by responsibility:
//
//   - worker (hosts a WhatsApp session and its calls, produces events):
//     sessionmanager.go, session.go, whatsapp.go, callrouting.go,
//     bridge.go, webrtc.go
//   - front  (serves HTTP/SSE to the operator): server.go, routes.go,
//     handlers_session.go, handlers_call.go, auth.go, authlogin.go,
//     contacts.go, history.go, openapi.go, ratelimit.go
//   - events (the worker->front seam): broker.go, callregistry.go, webhook.go
//
// Configuration and preflight checks live in sibling packages
// internal/app/config and internal/app/doctor.
package app
