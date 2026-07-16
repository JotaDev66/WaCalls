package app

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"wacalls/internal/app/events"
	"wacalls/internal/app/session"

	"gopkg.in/yaml.v3"
)

func TestOpenAPISpecMatchesRoutes(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPISpec, &doc); err != nil {
		t.Fatal(err)
	}
	httpMethods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	spec := map[string]bool{}
	for p, ops := range doc.Paths {
		for m := range ops {
			if httpMethods[m] {
				spec[strings.ToUpper(m)+" "+p] = true
			}
		}
	}
	served := map[string]bool{
		"GET /healthz":          true,
		"GET /api/openapi.yaml": true,
		"GET /api/auth/status":  true,
		"POST /api/login":       true,
	}
	for _, rt := range apiRoutes {
		served[rt.method+" /api"+rt.path] = true
	}
	for k := range served {
		if !spec[k] {
			t.Errorf("route %s is not documented in openapi.yaml", k)
		}
	}
	for k := range spec {
		if !served[k] {
			t.Errorf("openapi.yaml documents %s but no route serves it", k)
		}
	}
}

func TestOpenAPIServedWithoutAuth(t *testing.T) {
	s := &Server{
		authorize: bearerAuthorizer("secret"),
		broker:    events.NewBroker(nil, slog.Default()),
		sessions:  session.NewManager(session.Deps{}),
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/openapi.yaml", nil))
	if rec.Code != 200 {
		t.Fatalf("spec must be public, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("content-type: %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "openapi:") {
		t.Fatalf("body must start with openapi:, got %q", rec.Body.String()[:40])
	}
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	if rec.Code != 401 {
		t.Fatalf("api must stay behind auth, got %d", rec.Code)
	}
}
