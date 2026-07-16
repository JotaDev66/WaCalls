package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wacalls/internal/app/session"
)

func (s *Server) handleContactList(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if !sess.IsPaired() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	out, err := sess.ContactList(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": out})
}

func statusForContactErr(err error) int {
	switch {
	case errors.Is(err, session.ErrNotOnWhatsApp):
		return http.StatusUnprocessableEntity
	case errors.Is(err, session.ErrAppStateSyncing):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) handleContactSave(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if !sess.IsPaired() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	phone := normalizePhone(body.Phone)
	name := strings.TrimSpace(body.Name)
	if phone == "" || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone and name required"})
		return
	}
	dto, err := sess.SaveContact(r.Context(), phone, name)
	if err != nil {
		writeJSON(w, statusForContactErr(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contact": dto})
}
