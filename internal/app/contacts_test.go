package app

import (
	"testing"

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
