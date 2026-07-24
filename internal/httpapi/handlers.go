package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"GoMental/internal/application"
)

func (s *Server) service() *application.Service { return s.host.Service() }

// GET /api/info — server identity + mode. (There is no Service.Info; this is
// adapter-synthesized so browsers/agents can detect server mode.)
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{
		"name":        "GoMental",
		"description": "Local-first OKF notes and knowledge graph",
		"mode":        "server",
		"workspace":   s.cfg.WorkspaceRoot,
		"readOnly":    s.readOnly,
		"git":         nil,
	}
	if s.gitManager != nil {
		info["git"] = gitStatusJSON(s.gitManager.Status())
	}
	writeJSON(w, http.StatusOK, info)
}

// POST /api/workspace/open — restricted to the configured workspace root.
func (s *Server) handleOpenWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root string `json:"root"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// In server mode only the configured root may be opened. An empty root is
	// treated as "reopen the configured workspace".
	root := req.Root
	if root == "" {
		root = s.cfg.WorkspaceRoot
	}
	if root != s.cfg.WorkspaceRoot {
		writeErrorStatus(w, http.StatusForbidden, "workspace.forbidden", "server mode may only open its configured workspace")
		return
	}
	dto, err := s.service().OpenWorkspace(r.Context(), root)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// GET /api/recent
func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	items, err := s.service().RecentWorkspaces(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// GET /api/notes — returns the full note array by default. When a `limit` query
// param is present it returns a paginated NotesPageDTO instead (offset, limit,
// sort, desc, tag, q), served from the SQLite note projection.
func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("limit") == "" && q.Get("offset") == "" {
		notes, err := s.service().ListNotes(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, notes)
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	page, err := s.service().ListNotesPage(r.Context(), application.ListNotesQueryDTO{
		Offset: offset,
		Limit:  limit,
		SortBy: q.Get("sort"),
		Desc:   q.Get("desc") == "true" || q.Get("desc") == "1",
		Tag:    q.Get("tag"),
		Search: q.Get("q"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// GET /api/notes/{id...}
func (s *Server) handleReadNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	note, err := s.service().ReadNote(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	// Expose the opaque version token as an ETag so clients can round-trip it via
	// If-Match for optimistic concurrency.
	if note.Version != "" {
		w.Header().Set("ETag", strconv.Quote(note.Version))
	}
	writeJSON(w, http.StatusOK, note)
}

// PUT /api/notes/{id...}
func (s *Server) handleSaveNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Content     string `json:"content"`
		BaseVersion string `json:"baseVersion"`
		Force       bool   `json:"force"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	req := application.SaveNoteRequest{ID: id, Content: body.Content, BaseVersion: body.BaseVersion, Force: body.Force}
	// An If-Match header takes precedence and drives optimistic concurrency:
	// If-Match: "<version>" requires the note to be unchanged; If-Match: * forces.
	if im := strings.TrimSpace(r.Header.Get("If-Match")); im != "" {
		if im == "*" {
			req.Force = true
		} else {
			req.BaseVersion = strings.Trim(im, `"`)
		}
	}
	note, err := s.service().SaveNote(r.Context(), req)
	if err != nil {
		// On a version conflict, surface the current server version in ETag so
		// the client can reload/merge (HTTP 412 via the error mapping).
		var appErr application.AppError
		if errors.As(err, &appErr) && appErr.Code == application.ErrExternalConflict && appErr.Detail != "" {
			w.Header().Set("ETag", strconv.Quote(appErr.Detail))
			s.recordAudit(r, "note.save", id, appErr.Detail, "conflict", "")
		} else {
			s.recordAudit(r, "note.save", id, "", "error", err.Error())
		}
		writeError(w, err)
		return
	}
	if note.Version != "" {
		w.Header().Set("ETag", strconv.Quote(note.Version))
	}
	s.recordAudit(r, "note.save", note.ID, note.Version, "ok", "")
	writeJSON(w, http.StatusOK, note)
}

// DELETE /api/notes/{id...}
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.service().DeleteNote(r.Context(), id); err != nil {
		s.recordAudit(r, "note.delete", id, "", "error", err.Error())
		writeError(w, err)
		return
	}
	s.recordAudit(r, "note.delete", id, "", "ok", "")
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "deleted"})
}

// POST /api/notes/move
func (s *Server) handleMoveNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		NewID string `json:"newId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	note, err := s.service().MoveNote(r.Context(), application.MoveNoteRequest{ID: body.ID, NewID: body.NewID})
	if err != nil {
		s.recordAudit(r, "note.move", body.ID, "", "error", err.Error())
		writeError(w, err)
		return
	}
	s.recordAudit(r, "note.move", body.ID, note.Version, "ok", note.ID)
	writeJSON(w, http.StatusOK, note)
}

// POST /api/import — SSRF-guarded before the core fetch.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req application.ImportURLRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateImportURLForSSRF(req.URL); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "import.blocked", "import URL rejected: "+err.Error())
		return
	}
	note, err := s.service().ImportURL(r.Context(), req)
	if err != nil {
		s.recordAudit(r, "note.import", "", "", "error", err.Error())
		writeError(w, err)
		return
	}
	s.recordAudit(r, "note.import", note.ID, note.Version, "ok", req.URL)
	writeJSON(w, http.StatusOK, note)
}

// POST /api/assets/{id...} — note ID from the path; other fields from the body.
func (s *Server) handleSaveAsset(w http.ResponseWriter, r *http.Request) {
	var req application.SaveNoteAssetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.NoteID = r.PathValue("id")
	resp, err := s.service().SaveNoteAsset(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/assets/{id...}?path=relative/asset.png
func (s *Server) handleLoadAsset(w http.ResponseWriter, r *http.Request) {
	req := application.NoteAssetRequest{NoteID: r.PathValue("id"), Path: r.URL.Query().Get("path")}
	dataURL, err := s.service().LoadNoteAssetDataURL(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dataUrl": dataURL})
}

// POST /api/search
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req application.SearchQueryDTO
	if !decodeJSON(w, r, &req) {
		return
	}
	results, err := s.service().Search(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// POST /api/graph
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	var req application.GraphFilterDTO
	if !decodeJSON(w, r, &req) {
		return
	}
	graph, err := s.service().FullGraph(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

// POST /api/graph/query
func (s *Server) handleGraphQuery(w http.ResponseWriter, r *http.Request) {
	var req application.GraphQueryDTO
	if !decodeJSON(w, r, &req) {
		return
	}
	graph, err := s.service().GraphQuery(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

// GET /api/backlinks/{id...}
func (s *Server) handleBacklinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.service().Backlinks(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, links)
}

// GET /api/neighborhood/{id...}?depth=1
func (s *Server) handleNeighborhood(w http.ResponseWriter, r *http.Request) {
	depth := 1
	if raw := r.URL.Query().Get("depth"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			depth = parsed
		}
	}
	graph, err := s.service().Neighborhood(r.Context(), r.PathValue("id"), depth)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

// GET /api/graph/layout
func (s *Server) handleLoadLayout(w http.ResponseWriter, r *http.Request) {
	layout, err := s.service().LoadGraphLayout(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, layout)
}

// PUT /api/graph/layout
func (s *Server) handleSaveLayout(w http.ResponseWriter, r *http.Request) {
	var snapshot application.LayoutSnapshotDTO
	if !decodeJSON(w, r, &snapshot) {
		return
	}
	if err := s.service().SaveGraphLayout(r.Context(), snapshot); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// POST /api/rebuild
func (s *Server) handleRebuild(w http.ResponseWriter, r *http.Request) {
	result, err := s.service().Rebuild(r.Context())
	if err != nil {
		s.recordAudit(r, "workspace.rebuild", "", "", "error", err.Error())
		writeError(w, err)
		return
	}
	s.recordAudit(r, "workspace.rebuild", "", "", "ok", "")
	writeJSON(w, http.StatusOK, result)
}

// GET /api/ui-state
func (s *Server) handleLoadUIState(w http.ResponseWriter, r *http.Request) {
	state, err := s.service().LoadUIState(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// PUT /api/ui-state
func (s *Server) handleSaveUIState(w http.ResponseWriter, r *http.Request) {
	var state application.UIState
	if !decodeJSON(w, r, &state) {
		return
	}
	if err := s.service().SaveUIState(r.Context(), state); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// GET /api/settings
func (s *Server) handleLoadSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.service().LoadSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// PUT /api/settings
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings application.Settings
	if !decodeJSON(w, r, &settings) {
		return
	}
	if err := s.service().SaveSettings(r.Context(), settings); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
