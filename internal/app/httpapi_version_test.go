package app

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestVersionEndpoint(t *testing.T) {
	s := &Server{
		version:   "v9.9.9",
		authorize: bearerAuthorizer("secret"),
		broker:    NewBroker(nil, slog.Default()),
		sessions:  &SessionManager{sessions: map[string]*Session{}},
	}

	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "v9.9.9" {
		t.Fatalf("version: got %q, want v9.9.9", body.Version)
	}

	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/version", nil))
	if rec.Code != 401 {
		t.Fatalf("version must stay behind auth, got %d", rec.Code)
	}
}
