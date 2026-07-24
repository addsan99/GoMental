package httpapi

import (
	"net/http"

	"GoMental/internal/application"
	"GoMental/internal/auth"
)

// POST /api/notes — agent-ergonomic create with collision modes.
// (PUT /api/notes/{id} remains the upsert/save path.)
func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req application.CreateNoteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	note, err := s.service().CreateNote(r.Context(), req)
	if err != nil {
		s.recordAudit(r, "note.create", req.ID, "", "error", err.Error())
		writeError(w, err)
		return
	}
	if note.Version != "" {
		w.Header().Set("ETag", `"`+note.Version+`"`)
	}
	s.recordAudit(r, "note.create", note.ID, note.Version, "ok", req.Mode)
	writeJSON(w, http.StatusCreated, note)
}

// POST /api/keys — mint a new API key (admin). Returns the plaintext token once.
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		writeErrorStatus(w, http.StatusNotImplemented, "keys.disabled", "API key management is not enabled")
		return
	}
	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	role := auth.Role(body.Role)
	if role == "" {
		role = auth.RoleEditor // agents are editors by default
	}
	token, rec, err := s.keyStore.Create(body.Name, role)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "keys.create_failed", err.Error())
		return
	}
	s.recordAudit(r, "key.create", "", "", "ok", rec.ID+" role="+string(rec.Role))
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        rec.ID,
		"name":      rec.Name,
		"role":      rec.Role,
		"createdAt": rec.CreatedAt,
		"key":       token, // shown ONLY here
	})
}

// GET /api/keys — list keys without secrets (admin).
func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		writeErrorStatus(w, http.StatusNotImplemented, "keys.disabled", "API key management is not enabled")
		return
	}
	writeJSON(w, http.StatusOK, s.keyStore.List())
}

// DELETE /api/keys/{id} — revoke a key (admin).
func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		writeErrorStatus(w, http.StatusNotImplemented, "keys.disabled", "API key management is not enabled")
		return
	}
	id := r.PathValue("id")
	ok, err := s.keyStore.Revoke(id)
	if err != nil {
		writeErrorStatus(w, http.StatusInternalServerError, "keys.revoke_failed", err.Error())
		return
	}
	if !ok {
		writeErrorStatus(w, http.StatusNotFound, "keys.not_found", "no such key")
		return
	}
	s.recordAudit(r, "key.revoke", "", "", "ok", id)
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "revoked"})
}
