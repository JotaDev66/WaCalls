package app

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func startCallSession(jid *types.JID) *Session {
	mgr := NewManager(Deps{Log: slog.Default()})
	return mgr.NewSession("s1", "", &whatsmeow.Client{Store: &store.Device{ID: jid}})
}

func TestStartCallRejectsPhoneWithoutDigits(t *testing.T) {
	jid := types.NewJID("5511888880000", types.DefaultUserServer)
	sess := startCallSession(&jid)
	s := &Server{}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sessions/s1/calls", strings.NewReader(`{"phone":"abc"}`))
	s.doStartCall(sess, rec, r)

	if rec.Code != 400 {
		t.Fatalf("expected 400 for phone without digits, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid phone") {
		t.Fatalf("expected invalid phone error, got %q", rec.Body.String())
	}
}

func TestStartCallToleratesLegacyFields(t *testing.T) {
	jid := types.NewJID("5511888880000", types.DefaultUserServer)
	sess := startCallSession(&jid)
	s := &Server{}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sessions/s1/calls",
		strings.NewReader(`{"phone":"abc","duration_ms":300000,"record":true}`))
	s.doStartCall(sess, rec, r)

	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid phone") {
		t.Fatalf("legacy fields must decode fine and reach phone validation, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+55 (11) 99999-0000": "5511999990000",
		"abc":                 "",
		" +() -":              "",
		"5511999990000":       "5511999990000",
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}
