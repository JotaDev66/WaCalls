package app

import (
	"cmp"
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type contactDTO struct {
	JID   string `json:"jid"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

func contactsFromStore(raw map[types.JID]types.ContactInfo) []contactDTO {
	out := make([]contactDTO, 0, len(raw))
	for id, info := range raw {
		if id.Server != types.DefaultUserServer || id.User == "" {
			continue
		}
		out = append(out, contactDTO{
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
	s.mgr.broker.setCallPhoto(callID, info.URL)
}

func (s *Server) handleContactList(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	raw, err := sess.client.Store.Contacts.GetAllContacts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contactsFromStore(raw)})
}
