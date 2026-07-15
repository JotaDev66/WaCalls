package app

import (
	"cmp"
	"net/http"
	"sort"
	"strings"

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
