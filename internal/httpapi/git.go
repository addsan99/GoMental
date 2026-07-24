package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

	"GoMental/internal/auth"
	"GoMental/internal/gitsync"
)

// GitManager is the subset of *gitsync.Manager the HTTP layer needs. Kept as an
// interface so tests can inject a fake and the server stays decoupled from the
// concrete manager construction. It is non-nil only in git-viewer mode.
type GitManager interface {
	Sync(ctx context.Context) (gitsync.Result, error)
	Status() gitsync.Status
}

// registerGitRoutes mounts the git-viewer endpoints when a manager is present.
// Called from buildHandler. POST /api/git/sync is intentionally not wrapped in
// s.gate: it authenticates admin-key OR webhook-secret inside the handler so a
// git host webhook (which holds no API key) can trigger a pull.
func (s *Server) registerGitRoutes(mux *http.ServeMux) {
	if s.gitManager == nil {
		return
	}
	mux.HandleFunc("POST /api/git/sync", s.handleGitSync)
	mux.HandleFunc("GET /api/git/status", s.gate(auth.RoleViewer, s.handleGitStatus))
}

// handleGitSync triggers a fetch+reset of the working copy. The existing
// workspace watcher then reconciles content and emits note/graph events; this
// handler only advances git and reports the commit range.
func (s *Server) handleGitSync(w http.ResponseWriter, r *http.Request) {
	if s.gitManager == nil {
		writeErrorStatus(w, http.StatusNotFound, "git.disabled", "server is not in git-viewer mode")
		return
	}
	if !s.authorizeGitSync(r) {
		writeErrorStatus(w, http.StatusForbidden, "git.forbidden", "git sync requires an admin key or a valid webhook token")
		return
	}
	result, err := s.gitManager.Sync(r.Context())
	if err != nil {
		writeErrorStatus(w, http.StatusBadGateway, "git.sync_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"oldCommit": result.OldCommit,
		"newCommit": result.NewCommit,
		"changed":   len(result.Changed),
		"deleted":   len(result.Deleted),
	})
}

// handleGitStatus returns the current sync state (viewer-gated).
func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	if s.gitManager == nil {
		writeErrorStatus(w, http.StatusNotFound, "git.disabled", "server is not in git-viewer mode")
		return
	}
	writeJSON(w, http.StatusOK, gitStatusJSON(s.gitManager.Status()))
}

// authorizeGitSync permits the call when the actor already holds the admin role
// (a real admin API key, or trust-all) or presents the configured webhook
// secret via the X-GoMental-Token header or ?token= query parameter.
func (s *Server) authorizeGitSync(r *http.Request) bool {
	if actorFrom(r.Context()).Can(auth.RoleAdmin) {
		return true
	}
	if s.gitWebhookSecret == "" {
		return false
	}
	presented := r.Header.Get("X-GoMental-Token")
	if presented == "" {
		presented = r.URL.Query().Get("token")
	}
	return presented != "" &&
		subtle.ConstantTimeCompare([]byte(presented), []byte(s.gitWebhookSecret)) == 1
}

// gitStatusJSON renders a gitsync.Status with the field names the SPA consumes
// (lowercase; lastSyncAt as an RFC3339 string or null).
func gitStatusJSON(st gitsync.Status) map[string]any {
	var lastSyncAt any
	if st.LastSyncAt != nil {
		lastSyncAt = st.LastSyncAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"remote":     st.Remote,
		"ref":        st.Ref,
		"commit":     st.Commit,
		"lastSyncAt": lastSyncAt,
		"lastError":  st.LastError,
		"syncing":    st.Syncing,
	}
}
