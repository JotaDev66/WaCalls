package app

import (
	"encoding/base64"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wacalls/internal/voip/core"
)

const (
	historyDefaultLimit   = 50
	historyMaxLimit       = 200
	historyExportPageSize = 500
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

func (s *Server) handleHistoryExport(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	rows, next, err := s.broker.historyRows(r.Context(), sess.id, historyExportPageSize, core.HistoryCursor{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="history-`+sess.id+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"callId", "direction", "peer", "owner", "startedAt", "endedAt", "endReason"})
	for {
		for i := range rows {
			_ = cw.Write(historyCSVRow(&rows[i]))
		}
		if next == (core.HistoryCursor{}) {
			break
		}
		rows, next, err = s.broker.historyRows(r.Context(), sess.id, historyExportPageSize, next)
		if err != nil {
			s.log.Error("history export aborted", "session_id", sess.id, "err", err)
			break
		}
	}
	cw.Flush()
}

func historyCSVRow(r *CallRecord) []string {
	owner := ""
	if r.Owner != nil {
		owner = *r.Owner
	}
	endedAt := ""
	if r.EndedAt != nil {
		endedAt = time.UnixMilli(*r.EndedAt).UTC().Format(time.RFC3339)
	}
	return []string{
		r.CallID, r.Direction, r.Peer, owner,
		time.UnixMilli(r.StartedAt).UTC().Format(time.RFC3339),
		endedAt, r.EndReason,
	}
}
