package app

import (
	"cmp"
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
