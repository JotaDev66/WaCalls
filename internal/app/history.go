package app

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"wacalls/internal/voip/core"
)

const (
	historyDefaultLimit = 50
	historyMaxLimit     = 200
)

func historyLimit(raw string) (int, error) {
	if raw == "" {
		return historyDefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errors.New("invalid limit")
	}
	if n > historyMaxLimit {
		return historyMaxLimit, nil
	}
	return n, nil
}

func encodeHistoryCursor(c core.HistoryCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.EndedAt, 10) + ":" + c.CallID))
}

func decodeHistoryCursor(raw string) (core.HistoryCursor, error) {
	if raw == "" {
		return core.HistoryCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return core.HistoryCursor{}, errors.New("invalid cursor")
	}
	endedAt, callID, ok := strings.Cut(string(b), ":")
	if !ok || callID == "" {
		return core.HistoryCursor{}, errors.New("invalid cursor")
	}
	e, err := strconv.ParseInt(endedAt, 10, 64)
	if err != nil {
		return core.HistoryCursor{}, errors.New("invalid cursor")
	}
	return core.HistoryCursor{EndedAt: e, CallID: callID}, nil
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	limit, err := historyLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
		return
	}
	before, err := decodeHistoryCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
		return
	}
	rows, next, err := s.broker.historyRows(r.Context(), sess.id, limit, before)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]any{"calls": rows}
	if next != (core.HistoryCursor{}) {
		resp["nextCursor"] = encodeHistoryCursor(next)
	}
	writeJSON(w, http.StatusOK, resp)
}
