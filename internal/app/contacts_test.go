package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"wacalls/internal/app/session"
	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func mkJID(user, server string) types.JID { return types.NewJID(user, server) }

type fakePhotos struct{ m map[string]core.ContactPhoto }

func (f fakePhotos) Get(ctx context.Context, sid, jid string) (core.ContactPhoto, bool, error) {
	p, ok := f.m[jid]
	return p, ok, nil
}
func (f fakePhotos) GetMany(ctx context.Context, sid string, jids []string) (map[string]core.ContactPhoto, error) {
	out := map[string]core.ContactPhoto{}
	for _, j := range jids {
		if p, ok := f.m[j]; ok {
			out[j] = p
		}
	}
	return out, nil
}
func (f fakePhotos) Upsert(ctx context.Context, p core.ContactPhoto) error { return nil }

type fakeContacts struct {
	all map[types.JID]types.ContactInfo
}

func (f fakeContacts) GetAllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	return f.all, nil
}
func (f fakeContacts) GetContact(ctx context.Context, u types.JID) (types.ContactInfo, error) {
	if info, ok := f.all[u]; ok {
		return info, nil
	}
	return types.ContactInfo{}, nil
}
func (f fakeContacts) PutPushName(ctx context.Context, u types.JID, n string) (bool, string, error) {
	return false, "", nil
}
func (f fakeContacts) PutBusinessName(ctx context.Context, u types.JID, n string) (bool, string, error) {
	return false, "", nil
}
func (f fakeContacts) PutContactName(ctx context.Context, u types.JID, full, first string) error {
	return nil
}
func (f fakeContacts) PutAllContactNames(ctx context.Context, c []store.ContactEntry) error {
	return nil
}
func (f fakeContacts) PutManyRedactedPhones(ctx context.Context, e []store.RedactedPhoneEntry) error {
	return nil
}

func contactsServer(paired bool, all map[types.JID]types.ContactInfo) *Server {
	dev := &store.Device{Contacts: fakeContacts{all: all}}
	if paired {
		owner := types.NewJID("owner", types.DefaultUserServer)
		dev.ID = &owner
	}
	mgr := session.NewManager(session.Deps{Log: slog.Default()})
	mgr.NewSession("s1", "", &whatsmeow.Client{Store: dev})
	return &Server{
		authorize: bearerAuthorizer(""),
		sessions:  mgr,
	}
}

func TestContactListOK(t *testing.T) {
	s := contactsServer(true, map[types.JID]types.ContactInfo{
		mkJID("5511999998888", types.DefaultUserServer): {FirstName: "Alice"},
		mkJID("hidden", types.HiddenUserServer):         {FullName: "LID"},
	})
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/contacts", nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Contacts []session.Contact `json:"contacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Contacts) != 1 || body.Contacts[0].Phone != "5511999998888" {
		t.Fatalf("want 1 dialable Alice, got %+v", body.Contacts)
	}
}

func TestContactListEmptyIsJSONArray(t *testing.T) {
	s := contactsServer(true, map[types.JID]types.ContactInfo{})
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/contacts", nil))
	if rec.Code != 200 || rec.Body.String() != "{\"contacts\":[]}\n" {
		t.Fatalf("empty must be JSON array, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestContactListNotPaired(t *testing.T) {
	s := contactsServer(false, map[types.JID]types.ContactInfo{})
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/contacts", nil))
	if rec.Code != 503 {
		t.Fatalf("unpaired: want 503, got %d", rec.Code)
	}
}

func TestContactListUnknownSession(t *testing.T) {
	s := contactsServer(true, nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/ghost/contacts", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown session: want 404, got %d", rec.Code)
	}
}

func TestContactListWithPhoto(t *testing.T) {
	jid := mkJID("5511999998888", types.DefaultUserServer)
	owner := types.NewJID("owner", types.DefaultUserServer)
	dev := &store.Device{ID: &owner, Contacts: fakeContacts{all: map[types.JID]types.ContactInfo{jid: {FirstName: "Alice"}}}}
	mgr := session.NewManager(session.Deps{Photos: fakePhotos{m: map[string]core.ContactPhoto{jid.String(): {URL: "http://cdn/pic.jpg"}}}, Log: slog.Default()})
	mgr.NewSession("s1", "", &whatsmeow.Client{Store: dev})
	s := &Server{
		authorize: bearerAuthorizer(""),
		sessions:  mgr,
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/contacts", nil))
	var body struct {
		Contacts []session.Contact `json:"contacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Contacts) != 1 || body.Contacts[0].PhotoURL != "http://cdn/pic.jpg" {
		t.Fatalf("want photoUrl, got %+v", body.Contacts)
	}
}

func TestStatusForContactErr(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{session.ErrNotOnWhatsApp, 422},
		{session.ErrAppStateSyncing, 503},
		{errors.New("boom"), 500},
		{fmt.Errorf("wrap: %w", session.ErrNotOnWhatsApp), 422},
	}
	for _, c := range cases {
		if got := statusForContactErr(c.err); got != c.want {
			t.Errorf("statusForContactErr(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestContactSaveUnknownSession(t *testing.T) {
	s := contactsServer(true, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sessions/ghost/contacts", strings.NewReader(`{"phone":"5511","name":"x"}`))
	s.routes().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestContactSaveNotPaired(t *testing.T) {
	s := contactsServer(false, map[types.JID]types.ContactInfo{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sessions/s1/contacts", strings.NewReader(`{"phone":"5511","name":"x"}`))
	s.routes().ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestContactSaveBadBody(t *testing.T) {
	for _, body := range []string{`{"phone":"","name":"x"}`, `{"phone":"5511","name":"  "}`} {
		s := contactsServer(true, map[types.JID]types.ContactInfo{})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/sessions/s1/contacts", strings.NewReader(body))
		s.routes().ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("body %q: want 400, got %d", body, rec.Code)
		}
	}
}
