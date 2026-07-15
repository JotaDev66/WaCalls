package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

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

type fakeLIDs struct{ m map[types.JID]types.JID }

func (f fakeLIDs) GetPNForLID(ctx context.Context, lid types.JID) (types.JID, error) {
	return f.m[lid], nil
}
func (f fakeLIDs) GetLIDForPN(ctx context.Context, pn types.JID) (types.JID, error) {
	return types.JID{}, nil
}
func (f fakeLIDs) PutLIDMapping(ctx context.Context, lid, jid types.JID) error { return nil }
func (f fakeLIDs) PutManyLIDMappings(ctx context.Context, m []store.LIDMapping) error {
	return nil
}
func (f fakeLIDs) GetManyLIDsForPNs(ctx context.Context, pns []types.JID) (map[types.JID]types.JID, error) {
	return nil, nil
}

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

func TestContactListWithPhoto(t *testing.T) {
	jid := mkJID("5511999998888", types.DefaultUserServer)
	owner := types.NewJID("owner", types.DefaultUserServer)
	dev := &store.Device{ID: &owner, Contacts: fakeContacts{all: map[types.JID]types.ContactInfo{jid: {FirstName: "Alice"}}}}
	s := &Server{
		authorize: bearerAuthorizer(""),
		photos:    fakePhotos{m: map[string]core.ContactPhoto{jid.String(): {URL: "http://cdn/pic.jpg"}}},
		sessions: &SessionManager{sessions: map[string]*Session{
			"s1": {id: "s1", client: &whatsmeow.Client{Store: dev}},
		}},
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions/s1/contacts", nil))
	var body struct {
		Contacts []contactDTO `json:"contacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Contacts) != 1 || body.Contacts[0].PhotoURL != "http://cdn/pic.jpg" {
		t.Fatalf("want photoUrl, got %+v", body.Contacts)
	}
}

func TestEnrichPeers(t *testing.T) {
	rows := []CallRecord{{Peer: "5511@s.whatsapp.net"}, {Peer: "9999@s.whatsapp.net"}}
	names := map[string]string{"5511@s.whatsapp.net": "Alice"}
	photos := map[string]core.ContactPhoto{"5511@s.whatsapp.net": {URL: "u1"}}
	enrichPeers(rows, names, photos)
	if rows[0].PeerName != "Alice" || rows[0].PeerPhotoURL != "u1" {
		t.Fatalf("row0 not enriched: %+v", rows[0])
	}
	if rows[1].PeerName != "" || rows[1].PeerPhotoURL != "" {
		t.Fatalf("row1 should stay empty: %+v", rows[1])
	}
}

func TestResolvePeerJID(t *testing.T) {
	pn := mkJID("5511999998888", types.DefaultUserServer)
	lid := types.NewJID("62440234549366", types.HiddenUserServer)
	cli := &whatsmeow.Client{Store: &store.Device{LIDs: fakeLIDs{m: map[types.JID]types.JID{lid: pn}}}}
	if got := resolvePeerJID(context.Background(), cli, lid); got != pn {
		t.Fatalf("lid should map to pn, got %s", got)
	}
	if got := resolvePeerJID(context.Background(), cli, pn); got != pn {
		t.Fatalf("pn should be unchanged, got %s", got)
	}
	unmapped := types.NewJID("99999", types.HiddenUserServer)
	if got := resolvePeerJID(context.Background(), cli, unmapped); got != unmapped {
		t.Fatalf("unmapped lid should be unchanged, got %s", got)
	}
}

func TestResolvePeerName(t *testing.T) {
	jid := mkJID("5511", types.DefaultUserServer)
	cli := &whatsmeow.Client{Store: &store.Device{Contacts: fakeContacts{all: map[types.JID]types.ContactInfo{
		jid: {Found: true, FullName: "Alice"},
	}}}}
	if got := resolvePeerName(context.Background(), cli, jid); got != "Alice" {
		t.Fatalf("want Alice, got %q", got)
	}
	if got := resolvePeerName(context.Background(), cli, mkJID("9999", types.DefaultUserServer)); got != "" {
		t.Fatalf("want empty for unknown contact, got %q", got)
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ in, full, first string }{
		{"Alice Souza", "Alice Souza", "Alice"},
		{"  Bob  ", "Bob", "Bob"},
		{"  Ana   Paula  Lima ", "Ana   Paula  Lima", "Ana"},
		{"", "", ""},
	}
	for _, c := range cases {
		full, first := splitName(c.in)
		if full != c.full || first != c.first {
			t.Errorf("splitName(%q) = (%q,%q), want (%q,%q)", c.in, full, first, c.full, c.first)
		}
	}
}

func TestBuildContactPatch(t *testing.T) {
	jid := mkJID("5511999998888", types.DefaultUserServer)
	p := buildContactPatch(jid, "Alice Souza", "Alice")
	if p.Type != appstate.WAPatchCriticalUnblockLow {
		t.Fatalf("type = %v", p.Type)
	}
	if len(p.Mutations) != 1 {
		t.Fatalf("mutations = %d", len(p.Mutations))
	}
	m := p.Mutations[0]
	if !slices.Equal(m.Index, []string{"contact", jid.String()}) {
		t.Fatalf("index = %v", m.Index)
	}
	if m.Version != 2 {
		t.Fatalf("version = %d", m.Version)
	}
	ca := m.Value.GetContactAction()
	if ca.GetFullName() != "Alice Souza" || ca.GetFirstName() != "Alice" {
		t.Fatalf("action = %+v", ca)
	}
}
