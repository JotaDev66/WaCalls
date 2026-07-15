package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func mkJID(user, server string) types.JID { return types.NewJID(user, server) }

func TestContactsFromStoreFiltersAndSorts(t *testing.T) {
	raw := map[types.JID]types.ContactInfo{
		mkJID("5511888887777", types.DefaultUserServer): {FullName: "Bruno"},
		mkJID("5511999998888", types.DefaultUserServer): {FirstName: "Alice"},
		mkJID("hidden1", types.HiddenUserServer):        {FullName: "LID Only"},
		mkJID("group1", types.GroupServer):              {FullName: "Group"},
		mkJID("", types.DefaultUserServer):              {FullName: "NoUser"},
	}
	out := contactsFromStore(raw)
	if len(out) != 2 {
		t.Fatalf("want 2 dialable, got %d: %+v", len(out), out)
	}
	if out[0].Name != "Alice" || out[0].Phone != "5511999998888" {
		t.Fatalf("want Alice first, got %+v", out[0])
	}
	if out[1].Name != "Bruno" {
		t.Fatalf("want Bruno second, got %+v", out[1])
	}
	if out[0].JID != "5511999998888@s.whatsapp.net" {
		t.Fatalf("jid string: %q", out[0].JID)
	}
}

func TestContactsFromStoreNameFallback(t *testing.T) {
	raw := map[types.JID]types.ContactInfo{
		mkJID("111", types.DefaultUserServer): {PushName: "Pushy"},
		mkJID("222", types.DefaultUserServer): {},
	}
	out := contactsFromStore(raw)
	byPhone := map[string]contactDTO{out[0].Phone: out[0], out[1].Phone: out[1]}
	if byPhone["111"].Name != "Pushy" {
		t.Fatalf("want PushName fallback, got %q", byPhone["111"].Name)
	}
	if byPhone["222"].Name != "222" {
		t.Fatalf("want phone as last-resort name, got %q", byPhone["222"].Name)
	}
}

func TestContactsFromStoreEmptyIsNonNil(t *testing.T) {
	out := contactsFromStore(map[types.JID]types.ContactInfo{})
	if out == nil {
		t.Fatal("must be non-nil slice so JSON encodes []")
	}
}

type fakeContacts struct {
	all map[types.JID]types.ContactInfo
}

func (f fakeContacts) GetAllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	return f.all, nil
}
func (f fakeContacts) GetContact(ctx context.Context, u types.JID) (types.ContactInfo, error) {
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
	return &Server{
		authorize: bearerAuthorizer(""),
		sessions: &SessionManager{sessions: map[string]*Session{
			"s1": {id: "s1", client: &whatsmeow.Client{Store: dev}},
		}},
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
		Contacts []contactDTO `json:"contacts"`
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
