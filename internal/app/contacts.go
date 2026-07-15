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
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	PhotoURL string `json:"photoUrl,omitempty"`
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
	out := contactsFromStore(raw)
	if s.photos != nil && len(out) > 0 {
		jids := make([]string, len(out))
		for i := range out {
			jids[i] = out[i].JID
		}
		if photos, err := s.photos.GetMany(r.Context(), sess.id, jids); err == nil {
			for i := range out {
				out[i].PhotoURL = photos[out[i].JID].URL
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": out})
}

func enrichPeers(rows []CallRecord, names map[string]string, photos map[string]core.ContactPhoto) {
	for i := range rows {
		if n, ok := names[rows[i].Peer]; ok {
			rows[i].PeerName = n
		}
		rows[i].PeerPhotoURL = photos[rows[i].Peer].URL
	}
}

func (s *Server) enrichHistoryPeers(ctx context.Context, sess *Session, rows []CallRecord) {
	if len(rows) == 0 {
		return
	}
	names := map[string]string{}
	if sess.client != nil && sess.client.Store != nil && sess.client.Store.Contacts != nil {
		if all, err := sess.client.Store.Contacts.GetAllContacts(ctx); err == nil {
			for jid, info := range all {
				names[jid.String()] = cmp.Or(info.FullName, info.FirstName, info.PushName, info.BusinessName)
			}
		}
	}
	photos := map[string]core.ContactPhoto{}
	if s.photos != nil {
		peers := make([]string, len(rows))
		for i := range rows {
			peers[i] = rows[i].Peer
		}
		if m, err := s.photos.GetMany(ctx, sess.id, peers); err == nil {
			photos = m
		}
	}
	enrichPeers(rows, names, photos)
}
