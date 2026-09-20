package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"wacalls/internal/voip/media"

	"github.com/polymorfa/hypermeow/types"
	meowcaller "github.com/purpshell/meowcaller"
)

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/sessions", s.handleSessionList)
	mux.HandleFunc("POST /api/sessions", s.handleSessionCreate)
	mux.HandleFunc("DELETE /api/sessions/{sid}", s.handleSessionDelete)
	mux.HandleFunc("POST /api/sessions/{sid}/logout", s.handleSessionLogout)
	mux.HandleFunc("POST /api/sessions/{sid}/pair", s.handleSessionPair)
	mux.HandleFunc("GET /api/sessions/{sid}/calls", s.handleCallsCount)
	mux.HandleFunc("POST /api/sessions/{sid}/calls", s.handleStartCall)
	mux.HandleFunc("GET /api/sessions/{sid}/calls/{id}", s.handleCallGet)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/webrtc", s.handleWebRTC)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/webrtc/renegotiate", s.handleRenegotiate)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/video/start", s.handleVideoStart)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/video/stop", s.handleVideoStop)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/video/accept", s.handleVideoAccept)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/accept", s.handleAccept)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/reject", s.handleReject)
	mux.HandleFunc("DELETE /api/sessions/{sid}/calls/{id}", s.handleEndCall)
	mux.HandleFunc("PUT /api/sessions/{sid}/calls/{id}/hold", s.handleHold)
	mux.HandleFunc("PUT /api/sessions/{sid}/calls/{id}/unhold", s.handleUnhold)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/pickup", s.handlePickup)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/transfer", s.handleTransfer)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/reaction", s.handleReaction)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/hand", s.handleHandRaise)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/participants", s.handleAddParticipant)
	mux.HandleFunc("GET /api/sessions/{sid}/history", s.handleHistory)

	mux.HandleFunc("GET /api/events", s.handleEvents)

	if s.staticDir != "" {
		if _, err := os.Stat(s.staticDir); err == nil {
			mux.Handle("/", http.FileServer(http.Dir(s.staticDir)))
		}
	}
	return withCORS(withAPIKey(mux))
}

// withAPIKey trava toda requisição atrás de X-API-Key quando WACALLS_API_KEY
// está setado no ambiente; sem setar (padrão), a API continua aberta, igual
// ao comportamento documentado do deploy de referência.
func withAPIKey(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := os.Getenv("WACALLS_API_KEY")
		if want == "" || r.Method == http.MethodOptions || r.Header.Get("X-API-Key") == want {
			h.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing X-API-Key"})
	})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Client-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func clientID(r *http.Request) string {
	if id := r.Header.Get("X-Client-Id"); id != "" {
		return id
	}
	return r.URL.Query().Get("clientId")
}

// callOwnerAllows reports whether owner may act on call id: true if the call
// is unclaimed (Owner == nil) or owner is the current owner.
func (s *server) callOwnerAllows(id, owner string) bool {
	rec, ok := s.broker.getCall(id)
	if !ok {
		return true // no record yet; let the per-endpoint 404 handle it
	}
	return rec.Owner == nil || *rec.Owner == owner
}

func (s *server) sessionByID(w http.ResponseWriter, sid string) *Session {
	sess, ok := s.sessions.Get(sid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return nil
	}
	return sess
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.broker.serveSSE(w, r, clientID(r))
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"maxCallsPerSession": s.sessions.maxCalls})
}

func (s *server) handleCallsCount(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"active": sess.reg.count(), "maxCallsPerSession": s.sessions.maxCalls,
		})
	}
}

func (s *server) handleCallGet(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doCallGet(sess, w, r)
	}
}

func (s *server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessions.infos()})
}

func (s *server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Session"
	}
	id, err := s.sessions.Create(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Delete(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Logout(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionPair(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Pair(r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleStartCall(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doStartCall(sess, w, r)
	}
}

func (s *server) handleWebRTC(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doWebRTC(sess, w, r)
	}
}

func (s *server) handleRenegotiate(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doRenegotiate(sess, w, r)
	}
}

func (s *server) handleVideoStart(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doVideoStart(sess, w, r)
	}
}

func (s *server) handleVideoStop(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doVideoStop(sess, w, r)
	}
}

func (s *server) handleVideoAccept(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doVideoAccept(sess, w, r)
	}
}

func (s *server) handleAccept(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doAccept(sess, w, r)
	}
}

func (s *server) handleReject(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doReject(sess, w, r)
	}
}

func (s *server) handleEndCall(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doEndCall(sess, w, r)
	}
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		writeJSON(w, http.StatusOK, map[string]any{"rows": s.broker.historyRows(sess.id, 50)})
	}
}

func (s *server) handleReaction(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doReaction(sess, w, r)
	}
}

func (s *server) handleHandRaise(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doHandRaise(sess, w, r)
	}
}

func (s *server) handleAddParticipant(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doAddParticipant(sess, w, r)
	}
}

func (s *server) handleHold(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doHold(sess, w, r)
	}
}

func (s *server) handleUnhold(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doUnhold(sess, w, r)
	}
}

func (s *server) handlePickup(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doPickup(sess, w, r)
	}
}

func (s *server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doTransfer(sess, w, r)
	}
}

func (s *server) doStartCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Phone      string `json:"phone"`
		Group      string `json:"group"`
		DurationMs int    `json:"duration_ms"`
		Record     bool   `json:"record"`
		Video      bool   `json:"video"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	group := strings.TrimSpace(body.Group)
	if strings.TrimSpace(body.Phone) == "" && group == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone or group required"})
		return
	}
	owner := clientID(r)
	if !s.broker.tryReserveOwner(owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	defer s.broker.releaseReservation(owner)

	if max := s.sessions.maxCalls; max > 0 && sess.reg.count() >= max {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "max concurrent calls"})
		return
	}

	var callID, peerStr string
	var err error
	if group != "" {
		callID, err = sess.startOutgoingGroup(r.Context(), group, body.Video)
		peerStr = group
	} else {
		peer := types.NewJID(normalizePhone(body.Phone), types.DefaultUserServer)
		callID, err = sess.startOutgoing(r.Context(), peer, body.Video, nil)
		peerStr = peer.String()
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	callMedia := "audio"
	if body.Video {
		callMedia = "video"
	}
	s.broker.upsertCall(CallRecord{
		SessionID: sess.id, CallID: callID, Owner: &owner, Direction: "outbound", Peer: peerStr,
		Media: callMedia, StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
	})
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": callID}})
}

func (s *server) doReaction(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if err := ac.call.SendReaction(body.Text); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Reação que a gente mesmo mandou não volta ecoada pelo OnReaction,
	// então emite aqui (sender="" marca como nossa, mesma convenção que o
	// client usa pra estado local vs. do peer em outros lugares).
	s.broker.emitReaction(sess.id, id, "", body.Text)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doHandRaise(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	var body struct {
		Raised bool `json:"raised"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if err := ac.call.SetHandRaised(body.Raised); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.emitHandRaise(sess.id, id, "", body.Raised)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doAddParticipant(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Target) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target required"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if err := ac.call.AddParticipant(r.Context(), normalizePhone(body.Target)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// callStateForAPI mapeia nosso CallStatus interno pros nomes de estado que a
// API pública documenta (batendo com o enum do spec OpenAPI de referência).
func callStateForAPI(status CallStatus) string {
	switch status {
	case StatusStarting:
		return "initiating"
	case StatusRinging:
		return "ringing"
	case StatusConnected:
		return "active"
	case StatusHeld:
		return "held"
	case StatusTransferring:
		return "transferring"
	case StatusEnded:
		return "ended"
	default:
		return string(status)
	}
}

func (s *server) doCallGet(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.broker.getCall(id)
	if !ok || rec.SessionID != sess.id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	direction := "outgoing"
	if rec.Direction == "inbound" {
		direction = "incoming"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": rec.CallID, "sid": rec.SessionID, "peer": rec.Peer,
		"state": callStateForAPI(rec.Status), "direction": direction,
	})
}

func (s *server) doWebRTC(sess *Session, w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	ac, ok := sess.reg.get(callID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	owner := clientID(r)
	if !s.callOwnerAllows(callID, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	var body struct {
		SDPOffer string `json:"sdp_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SDPOffer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp_offer required"})
		return
	}
	bridge, answer, err := NewBridge(body.SDPOffer, s.log)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	bridge.OnBrowserPCM = func(pcm []float32) {
		if ac.held.Load() {
			return // em hold: não bufferiza áudio do microfone que o peer não está ouvindo
		}
		ac.src.push(pcm)
	}
	bridge.OnBrowserVideo = func(f media.VideoFrame) {
		_ = ac.call.SendVideo(f.Data)
	}
	bridge.OnTerminalICE = func() {
		go sess.terminateCall(callID, "user_ended")
	}
	sess.setBridge(callID, bridge)

	ac.call.Play(ac.src)
	ac.call.Receive(meowcaller.SinkFunc(func(pcm []float32) { _ = bridge.WritePCM(pcm) }))
	// ac.videoSink já está plugado em Call.ReceiveVideo desde wireCall — só
	// falta dar o bridge do browser a ele agora que existe (ver
	// orientedVideoSink em bridge.go pra saber por quê não recriar o sink
	// aqui).
	ac.videoSink.attachBridge(bridge)

	writeJSON(w, http.StatusOK, map[string]string{"sdp_answer": answer})
}

// doRenegotiate aplica uma nova oferta SDP na MESMA peer connection já
// aberta por doWebRTC — usado quando o browser adiciona o data channel
// "vp8" no meio de uma chamada que começou só de áudio (upgrade pra
// vídeo). Recriar o bridge inteiro, como doWebRTC faz na conexão inicial,
// cortaria o áudio que já está fluindo.
func (s *server) doRenegotiate(sess *Session, w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(callID, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	bridge, ok := sess.reg.bridge(callID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no bridge connected for this call yet"})
		return
	}
	var body struct {
		SDPOffer string `json:"sdp_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SDPOffer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp_offer required"})
		return
	}
	answer, err := bridge.Renegotiate(body.SDPOffer)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sdp_answer": answer})
}

// doVideoStart pede o upgrade de áudio pra vídeo no protocolo do WhatsApp
// (Call.StartVideo). O browser já deve ter aberto o data channel "vp8"
// via doRenegotiate antes de chamar isso — meowcaller mantém o vídeo de
// saída represado internamente até o peer confirmar a transição.
func (s *server) doVideoStart(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if err := ac.call.StartVideo(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.setCallMedia(id, "video")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// doVideoStop para o vídeo de saída deste cliente, preservando áudio e o
// vídeo do peer (Call.StopVideo) — a chamada continua, só some a nossa câmera.
func (s *server) doVideoStop(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if err := ac.call.StopVideo(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.setCallMedia(id, "audio")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// doVideoAccept confirma um upgrade de vídeo PEDIDO PELO PEER (evento SSE
// call-peer-video com upgrade=true) — sem isso o vídeo dele fica represado
// no meowcaller e o app dele acaba desistindo depois de retentar algumas
// vezes. Diferente de doVideoStart (que pede UM upgrade nosso), aqui é
// Call.AcceptVideo(): confirma o dele sem mudar nosso próprio estado de
// saída. É acionado só quando o operador aceita a notificação na UI —
// nunca automaticamente (ver OnVideoState em session.go).
func (s *server) doVideoAccept(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	// A ordem importa. AcceptVideo() só libera RECEBER o vídeo do peer —
	// "without changing this client's independent outbound video state" (doc
	// do meowcaller) — mas por baixo (engine.go:transitionVideo, caso
	// VideoStateUpgradeAccept), se o NOSSO vídeo ainda não estiver ativo
	// (m.localVideo == false) ele manda um <video state="stopped"> pro peer
	// ANTES do aceite, "confirmando" que não vamos mandar vídeo. Como o
	// aceite na nossa UI reabre a câmera e manda quadros de verdade (ver
	// upgradeConnectionToVideo no frontend), StartVideo() PRECISA rodar
	// primeiro — assim m.localVideo já está true quando AcceptVideo() lê o
	// estado, e aquele "stopped" espúrio nunca é mandado. Na ordem errada
	// (Accept depois Start), o "stopped" chegava bem na hora que devíamos
	// estar pedindo pra ligar, e o vídeo nunca saía de verdade pro WhatsApp.
	if err := ac.call.StartVideo(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := ac.call.AcceptVideo(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.setCallMedia(id, "video")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doHold(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	rec, ok := s.broker.getCall(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if rec.Status != StatusConnected {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call is not active"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	// moh_url é aceito por compatibilidade de formato da API mas não é
	// implementado — decodificar uma URL de áudio remota arbitrária
	// precisaria de um decoder de áudio de verdade, que esta build não tem.
	// Hold sempre toca um tom local de 440 Hz.
	var body struct {
		MohURL string `json:"moh_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ac.held.Store(true)
	ac.call.Play(newHoldToneSource())
	cp, _ := s.broker.setStatus(id, StatusHeld)
	s.broker.emitCallHeld(sess.id, id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "state": callStateForAPI(cp.Status)})
}

func (s *server) doUnhold(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	rec, ok := s.broker.getCall(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if rec.Status != StatusHeld {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call is not held"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	ac.held.Store(false)
	ac.call.Play(ac.src) // retoma o bridge do microfone do browser já conectado em doWebRTC
	cp, _ := s.broker.setStatus(id, StatusConnected)
	s.broker.emitCallUnheld(sess.id, id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "state": callStateForAPI(cp.Status)})
}

// doPickup passa o bridge WebRTC de uma chamada ativa pra quem quer que
// chame esse endpoint, independente de quem seja o dono atual — esse é o
// ponto do pickup (handoff entre agentes). A chamada em si nunca muda de
// sessão; só seu bridge (fechado aqui) e seu dono rastreado pelo broker mudam.
func (s *server) doPickup(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	rec, ok := s.broker.getCall(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call in any session"})
		return
	}
	realSess, ok := s.sessions.Get(rec.SessionID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call's session no longer exists"})
		return
	}
	if oldB, found := realSess.reg.setBridge(id, nil); found && oldB != nil {
		oldB.CloseQuiet()
	}
	s.broker.forceSetOwner(id, owner)
	s.broker.emitCallPickedUp(rec.SessionID, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "state": callStateForAPI(rec.Status), "sessionId": rec.SessionID,
	})
}

// doTransfer implementa a transferência cega com fallback da API de
// referência: põe a chamada original em hold, toca o mesmo peer a partir de
// target_sid, e responde imediatamente com o ID da chamada de transferência
// enquanto finishTransfer observa o desfecho (aceite/rejeição/timeout) em
// segundo plano.
func (s *server) doTransfer(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	var body struct {
		TargetSid string `json:"target_sid"`
		MohURL    string `json:"moh_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.TargetSid) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_sid required"})
		return
	}
	if body.TargetSid == sess.id {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_sid must differ from sid"})
		return
	}
	targetSess, ok := s.sessions.Get(body.TargetSid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target session not found"})
		return
	}
	if targetSess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "target session not paired"})
		return
	}
	rec, ok := s.broker.getCall(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	if rec.Status != StatusConnected {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call is not active"})
		return
	}
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	peer, err := types.ParseJID(rec.Peer)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot parse call peer for transfer"})
		return
	}

	ac.held.Store(true)
	ac.call.Play(newHoldToneSource())
	s.broker.setStatus(id, StatusTransferring)
	s.broker.emitCallHeld(sess.id, id)

	endReason := make(chan string, 1)
	var endOnce sync.Once
	transferID, err := targetSess.startOutgoing(r.Context(), peer, false, func(reason string) {
		endOnce.Do(func() { endReason <- reason })
	})
	if err != nil {
		ac.held.Store(false)
		ac.call.Play(ac.src)
		s.broker.setStatus(id, StatusConnected)
		s.broker.emitCallUnheld(sess.id, id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.upsertCall(CallRecord{
		SessionID: targetSess.id, CallID: transferID, Owner: &owner, Direction: "outbound", Peer: rec.Peer,
		Media: "audio", StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
	})
	s.broker.emitTransferStarted(sess.id, id, transferID)

	accepted := make(chan struct{})
	if tac, ok := targetSess.reg.get(transferID); ok {
		var acceptOnce sync.Once
		tac.call.OnPeerAccept(func() { acceptOnce.Do(func() { close(accepted) }) })
	}

	go s.finishTransfer(sess, targetSess, id, transferID, accepted, endReason)

	writeJSON(w, http.StatusOK, map[string]any{
		"original_call_id": id, "transfer_call_id": transferID,
		"target_sid": targetSess.id, "state": "transferring",
	})
}

// finishTransfer observa a perna sainte de uma transferência cega até o
// fim: o peer aceitando encerra a chamada original; uma rejeição ou um
// timeout de 60s restaura ela do hold. Roda na própria goroutine —
// doTransfer já respondeu.
func (s *server) finishTransfer(origSess, targetSess *Session, originalID, transferID string, accepted <-chan struct{}, endReason <-chan string) {
	select {
	case <-accepted:
		if oac, ok := origSess.reg.get(originalID); ok {
			_ = oac.call.Hangup()
		}
		s.broker.emitTransferCompleted(origSess.id, originalID, transferID)
	case <-endReason:
		s.rollbackTransfer(origSess, originalID)
		s.broker.emitTransferFailed(origSess.id, originalID, transferID, "rejected")
	case <-time.After(60 * time.Second):
		if tac, ok := targetSess.reg.get(transferID); ok {
			_ = tac.call.Hangup()
		}
		s.rollbackTransfer(origSess, originalID)
		s.broker.emitTransferFailed(origSess.id, originalID, transferID, "timeout")
	}
}

// rollbackTransfer tira a chamada original do hold depois de uma tentativa
// de transferência falhada, se ela ainda existir (o operador pode ter
// desligado direto enquanto a transferência estava em andamento).
func (s *server) rollbackTransfer(sess *Session, id string) {
	ac, ok := sess.reg.get(id)
	if !ok {
		return
	}
	ac.held.Store(false)
	ac.call.Play(ac.src)
	s.broker.setStatus(id, StatusConnected)
	s.broker.emitCallUnheld(sess.id, id)
}

func (s *server) doAccept(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" && other != id {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	if !s.broker.setOwner(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "claimed by another client"})
		return
	}
	s.broker.emitIncomingClaimed(sess.id, id, owner)
	if err := ac.call.Answer(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": id}})
}

func (s *server) doReject(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	if ac, ok := sess.reg.get(id); ok {
		_ = ac.call.Reject()
	}
	sess.removeCall(id)
	s.broker.endCall(id, "declined")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doEndCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	owner := clientID(r)
	if !s.callOwnerAllows(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call claimed by another client"})
		return
	}
	if ac, ok := sess.reg.get(id); ok {
		_ = ac.call.Hangup()
	}
	sess.removeCall(id)
	s.broker.endCall(id, "user_ended")
	w.WriteHeader(http.StatusNoContent)
}

func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
