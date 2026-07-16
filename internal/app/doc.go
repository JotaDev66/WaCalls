// Package app is the HTTP/SSE front end for the operator. It serves HTTP/SSE
// and drives the WhatsApp worker through the sibling package
// internal/app/session, whose exported API (session.Manager and session.Session)
// is the only surface it touches; the compiler now enforces that boundary.
//
// Front files group by responsibility: server.go, routes.go,
// handlers_session.go, handlers_call.go, contacts.go, history.go, auth.go,
// authlogin.go, ratelimit.go, openapi.go, callrouting.go, webrtc.go.
//
// The worker (Session + Manager + call/contact commands + browser bridge) lives
// in internal/app/session. The SSE broker, live call registry, and webhook
// dispatcher live in internal/app/events; configuration and preflight checks in
// internal/app/config and internal/app/doctor.
package app
