// Package app is the HTTP/SSE front end and the WhatsApp session workers it
// drives. Files group by responsibility:
//
//   - front (serves HTTP/SSE to the operator): server.go, routes.go,
//     handlers_session.go, handlers_call.go, contacts.go, history.go,
//     auth.go, authlogin.go, ratelimit.go, openapi.go
//   - worker (hosts a WhatsApp session and its calls): sessionmanager.go,
//     session.go, session_commands.go, whatsapp.go, callrouting.go, bridge.go,
//     webrtc.go
//
// The SSE broker, live call registry, and webhook dispatcher live in the
// sibling package internal/app/events; configuration and preflight checks in
// internal/app/config and internal/app/doctor.
//
// Splitting the worker into its own package is intentionally deferred: the
// front now drives Session only through its command methods (session_commands.go),
// so what remains is a mechanical file-move, not an API change.
package app
