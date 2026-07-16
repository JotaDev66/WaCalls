// Package app is the HTTP/SSE front end and the WhatsApp session workers it
// drives. Files group by responsibility:
//
//   - front (serves HTTP/SSE to the operator): server.go, routes.go,
//     handlers_session.go, handlers_call.go, contacts.go, history.go,
//     auth.go, authlogin.go, ratelimit.go, openapi.go
//   - worker (hosts a WhatsApp session and its calls): sessionmanager.go,
//     session.go, whatsapp.go, callrouting.go, bridge.go, webrtc.go
//
// The SSE broker, live call registry, and webhook dispatcher live in the
// sibling package internal/app/events; configuration and preflight checks in
// internal/app/config and internal/app/doctor.
//
// Splitting the worker into its own package is intentionally deferred: the
// front still reaches into Session internals (the whatsmeow client), so a clean
// worker boundary needs command methods on Session first, not just a file move.
package app
