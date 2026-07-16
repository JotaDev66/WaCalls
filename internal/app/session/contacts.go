package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"wacalls/internal/app/events"
	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type Contact struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	PhotoURL string `json:"photoUrl,omitempty"`
}

func contactsFromStore(raw map[types.JID]types.ContactInfo) []Contact {
	out := make([]Contact, 0, len(raw))
	for id, info := range raw {
		if id.Server != types.DefaultUserServer || id.User == "" {
			continue
		}
		out = append(out, Contact{
			JID:   id.String(),
			Name:  cmp.Or(info.FullName, info.FirstName, info.PushName, info.BusinessName, id.User),
			Phone: id.User,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if li != lj {
			return li < lj
		}
		return out[i].Phone < out[j].Phone
	})
	return out
}

// resolvePeerJID maps a @lid peer (as seen on incoming calls) back to its phone-number JID via the
// whatsmeow LID store, so name and photo lookups (which are keyed by @s.whatsapp.net) match. Returns
// the input unchanged when it is not a LID or has no known mapping.
func resolvePeerJID(ctx context.Context, cli *whatsmeow.Client, raw types.JID) types.JID {
	if raw.Server != types.HiddenUserServer || cli == nil || cli.Store == nil || cli.Store.LIDs == nil {
		return raw
	}
	pn, err := cli.Store.LIDs.GetPNForLID(ctx, raw.ToNonAD())
	if err != nil || pn.IsEmpty() {
		return raw
	}
	return pn
}

func resolvePeerName(ctx context.Context, cli *whatsmeow.Client, jid types.JID) string {
	if cli == nil || cli.Store == nil || cli.Store.Contacts == nil {
		return ""
	}
	info, err := cli.Store.Contacts.GetContact(ctx, jid)
	if err != nil || !info.Found {
		return ""
	}
	return cmp.Or(info.FullName, info.FirstName, info.PushName, info.BusinessName)
}

func cachedPhotoURL(ctx context.Context, photos core.ContactPhotoStore, sessionID, jid string) string {
	if photos == nil {
		return ""
	}
	p, ok, err := photos.Get(ctx, sessionID, jid)
	if err != nil || !ok {
		return ""
	}
	return p.URL
}

func (s *Session) fetchOwnPhoto() {
	id := s.client.Store.ID
	if id == nil || s.mgr.photos == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	jid := id.ToNonAD()
	info, err := s.client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})
	if err != nil || info == nil || info.URL == "" {
		return
	}
	_ = s.mgr.photos.Upsert(ctx, core.ContactPhoto{
		SessionID: s.id, Jid: jid.String(), URL: info.URL, PictureID: info.ID, FetchedAt: time.Now().UnixMilli(),
	})
	s.mgr.broker.EmitSessionList(s.mgr.Infos())
}

func (s *Session) fetchPeerPhoto(jid types.JID, callID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := s.client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})
	if err != nil || info == nil || info.URL == "" {
		return
	}
	_ = s.mgr.photos.Upsert(ctx, core.ContactPhoto{
		SessionID: s.id, Jid: jid.String(), URL: info.URL, PictureID: info.ID, FetchedAt: time.Now().UnixMilli(),
	})
	s.mgr.broker.SetCallPhoto(callID, info.URL)
}

func (s *Session) ContactList(ctx context.Context) ([]Contact, error) {
	raw, err := s.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	out := contactsFromStore(raw)
	if s.mgr.photos != nil && len(out) > 0 {
		jids := make([]string, len(out))
		for i := range out {
			jids[i] = out[i].JID
		}
		if photos, err := s.mgr.photos.GetMany(ctx, s.id, jids); err == nil {
			for i := range out {
				out[i].PhotoURL = photos[out[i].JID].URL
			}
		}
	}
	return out, nil
}

func enrichPeers(rows []events.CallRecord, names map[string]string, photos map[string]core.ContactPhoto) {
	for i := range rows {
		if n, ok := names[rows[i].Peer]; ok {
			rows[i].PeerName = n
		}
		rows[i].PeerPhotoURL = photos[rows[i].Peer].URL
	}
}

func splitName(name string) (full, first string) {
	full = strings.TrimSpace(name)
	if fields := strings.Fields(full); len(fields) > 0 {
		first = fields[0]
	}
	return full, first
}

func buildContactPatch(jid types.JID, fullName, firstName string) appstate.PatchInfo {
	return appstate.PatchInfo{
		Type: appstate.WAPatchCriticalUnblockLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexContact, jid.String()},
			Version: 2,
			Value: &waSyncAction.SyncActionValue{
				ContactAction: &waSyncAction.ContactAction{
					FullName:  proto.String(fullName),
					FirstName: proto.String(firstName),
				},
			},
		}},
	}
}

type contactClient interface {
	IsOnWhatsApp(ctx context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error)
	SendAppState(ctx context.Context, patch appstate.PatchInfo) error
}

var (
	ErrNotOnWhatsApp   = errors.New("number not on WhatsApp")
	ErrAppStateSyncing = errors.New("app state not synced yet")
)

func upsertContact(ctx context.Context, cc contactClient, phone, name string) (Contact, error) {
	full, first := splitName(name)
	resp, err := cc.IsOnWhatsApp(ctx, []string{"+" + phone})
	if err != nil {
		return Contact{}, fmt.Errorf("checking whatsapp: %w", err)
	}
	if len(resp) == 0 || !resp[0].IsIn {
		return Contact{}, ErrNotOnWhatsApp
	}
	jid := resp[0].JID
	if err := cc.SendAppState(ctx, buildContactPatch(jid, full, first)); err != nil {
		if strings.Contains(err.Error(), "no app state keys found") {
			return Contact{}, fmt.Errorf("%w: %v", ErrAppStateSyncing, err)
		}
		return Contact{}, fmt.Errorf("sending app state: %w", err)
	}
	return Contact{JID: jid.String(), Name: full, Phone: jid.User}, nil
}

func (s *Session) SaveContact(ctx context.Context, phone, name string) (Contact, error) {
	return upsertContact(ctx, s.client, phone, name)
}

func (s *Session) EnrichHistoryPeers(ctx context.Context, rows []events.CallRecord) {
	if len(rows) == 0 {
		return
	}
	names := map[string]string{}
	if s.client != nil && s.client.Store != nil && s.client.Store.Contacts != nil {
		if all, err := s.client.Store.Contacts.GetAllContacts(ctx); err == nil {
			for jid, info := range all {
				names[jid.String()] = cmp.Or(info.FullName, info.FirstName, info.PushName, info.BusinessName)
			}
		}
	}
	photos := map[string]core.ContactPhoto{}
	if s.mgr.photos != nil {
		peers := make([]string, len(rows))
		for i := range rows {
			peers[i] = rows[i].Peer
		}
		if m, err := s.mgr.photos.GetMany(ctx, s.id, peers); err == nil {
			photos = m
		}
	}
	enrichPeers(rows, names, photos)
}
