package workspace

import "errors"

var (
	ErrInvalidWorkspaceRoot = errors.New("invalid workspace root")
	ErrInvalidNoteID        = errors.New("invalid note id")
	ErrPathEscapesWorkspace = errors.New("path escapes workspace")
	ErrReservedNoteID       = errors.New("reserved OKF document cannot be used as a concept note")
	ErrDuplicateNoteID      = errors.New("duplicate note id")
	ErrInvalidUTF8          = errors.New("note file is not valid UTF-8")
	ErrNoteAlreadyExists    = errors.New("note already exists")
	// ErrVersionConflict is returned by SaveIfUnchanged when the on-disk file
	// version differs from the expected version (optimistic-concurrency check).
	ErrVersionConflict = errors.New("note changed on disk since it was read")
)
