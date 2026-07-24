package httpapi

import (
	"context"
	"errors"
	"net/http"

	"GoMental/internal/auth"
)

type contextKey string

const actorContextKey contextKey = "gomental.actor"

// authMiddleware resolves the actor for every request via the configured
// Authenticator and stashes it in the request context. In trust-all mode this
// always yields the local admin actor and never rejects. A real authenticator
// returning auth.ErrUnauthorized produces a 401.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, err := s.auth.Authenticate(r)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthorized) {
				writeErrorStatus(w, http.StatusUnauthorized, "auth.unauthorized", "authentication required")
				return
			}
			writeErrorStatus(w, http.StatusInternalServerError, "auth.error", err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), actorContextKey, actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// actorFrom returns the actor attached by authMiddleware, or the trust-all local
// actor if none is present (defensive default).
func actorFrom(ctx context.Context) auth.Actor {
	if a, ok := ctx.Value(actorContextKey).(auth.Actor); ok {
		return a
	}
	return auth.LocalActor
}

// gate wraps a handler with a coarse role requirement. Under trust-all the
// resolved actor is admin, so every gate passes; with a real authenticator an
// insufficient role yields 403. The gate is keyed to the endpoint map in the
// Phase 17 router.
func (s *Server) gate(required auth.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !actorFrom(r.Context()).Can(required) {
			writeErrorStatus(w, http.StatusForbidden, "auth.forbidden", "insufficient role for this action")
			return
		}
		next(w, r)
	}
}

// contentWrite wraps a content-mutating handler so that, in read-only
// (git-viewer) mode, it is rejected with 403 before any work happens. The git
// working copy is a read replica of the source-of-truth repo; a write here would
// be silently discarded by the next fetch+reset, so we refuse it up front. View
// state (graph layout, ui-state) is not content and stays writable.
func (s *Server) contentWrite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.readOnly {
			writeErrorStatus(w, http.StatusForbidden, "workspace.read_only", "workspace is read-only: content is managed in git")
			return
		}
		next(w, r)
	}
}

// recordAudit appends a write action to the audit log (no-op if unconfigured).
func (s *Server) recordAudit(r *http.Request, action, noteID, version, result, detail string) {
	if s.audit == nil {
		return
	}
	actor := actorFrom(r.Context())
	_ = s.audit.Record(auth.AuditEntry{
		Actor:   actor.ID,
		Role:    string(actor.Role),
		Action:  action,
		NoteID:  noteID,
		Version: version,
		Result:  result,
		Detail:  detail,
	})
}
