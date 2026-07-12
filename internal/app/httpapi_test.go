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

func TestStartCallRejectsPhoneWithoutDigits(t *testing.T) {
	jid := types.NewJID("5511888880000", types.DefaultUserServer)
	sess := &Session{
		log:    slog.Default(),
		client: &whatsmeow.Client{Store: &store.Device{ID: &jid}},
	}
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
