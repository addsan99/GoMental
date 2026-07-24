package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"GoMental/internal/application"
)

// errorBody is the JSON shape returned for every error. The code string is
// preserved verbatim from application.AppError so clients can branch on it.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// statusForAppError maps an application.AppError code to an HTTP status. The
// mapping follows the Phase 17 plan: not_open -> 409, invalid_* -> 400,
// external_conflict -> 412, traversal/escape -> 400, everything else -> 500.
func statusForAppError(code string) int {
	switch {
	case code == application.ErrExternalConflict:
		return http.StatusPreconditionFailed // 412
	case code == application.ErrNoteExists:
		return http.StatusConflict // 409 (create-or-fail collision)
	case strings.HasSuffix(code, ".not_open"):
		return http.StatusConflict // 409
	case code == "notes.read_failed", code == "notes.list_failed":
		return http.StatusNotFound // best-effort: missing note reads
	case strings.Contains(code, "invalid") || strings.Contains(code, "escape") ||
		strings.Contains(code, "traversal") || strings.Contains(code, "unsupported") ||
		strings.Contains(code, "empty") || strings.Contains(code, "too_large") ||
		strings.Contains(code, "decode"):
		return http.StatusBadRequest // 400
	default:
		return http.StatusInternalServerError // 500
	}
}

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// writeError converts any error into a JSON error body with an appropriate
// HTTP status. application.AppError values are mapped by code; other errors
// become 500 with a generic code (unless overridden by status).
func writeError(w http.ResponseWriter, err error) {
	var appErr application.AppError
	if errors.As(err, &appErr) {
		writeJSON(w, statusForAppError(appErr.Code), errorBody{Code: appErr.Code, Message: appErr.Message, Detail: appErr.Detail})
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorBody{Code: "internal", Message: err.Error()})
}

// writeErrorStatus writes an error body with an explicit status and code
// (for adapter-level errors that never reach the core service, e.g. bad JSON).
func writeErrorStatus(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Code: code, Message: message})
}
