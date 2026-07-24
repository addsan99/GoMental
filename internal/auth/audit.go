package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry is one append-only audit record: who did what to which note, at
// what version, and whether it succeeded.
type AuditEntry struct {
	Time    string `json:"time"`
	Actor   string `json:"actor"`
	Role    string `json:"role"`
	Action  string `json:"action"`
	NoteID  string `json:"noteId,omitempty"`
	Version string `json:"version,omitempty"`
	Result  string `json:"result"`
	Detail  string `json:"detail,omitempty"`
}

// defaultMaxAuditBytes is the size at which the audit log rotates.
const defaultMaxAuditBytes int64 = 10 << 20 // 10 MiB

// AuditLog is an append-only JSON-lines log of write actions. It lives under the
// workspace metadata dir (never mixed into note content — Guardrail G4) so a
// plain checkout / the desktop app are unaffected. It rotates by size, keeping
// one previous generation (audit.log.1).
type AuditLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
}

// DefaultAuditPath returns the audit log path for a workspace root.
func DefaultAuditPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".workspace", "audit", "audit.log")
}

// OpenAuditLog prepares an audit log at path (creating parent dirs).
func OpenAuditLog(path string) (*AuditLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &AuditLog{path: path, maxBytes: defaultMaxAuditBytes}, nil
}

// Record appends one entry. A nil receiver is a no-op so callers need not
// branch on whether auditing is configured.
func (l *AuditLog) Record(entry AuditEntry) error {
	if l == nil {
		return nil
	}
	if entry.Time == "" {
		entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotateIfNeededLocked(int64(len(data) + 1))
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// rotateIfNeededLocked rotates the log to <path>.1 when the next write would push
// it past maxBytes. One previous generation is kept.
func (l *AuditLog) rotateIfNeededLocked(incoming int64) {
	if l.maxBytes <= 0 {
		return
	}
	info, err := os.Stat(l.path)
	if err != nil {
		return // no file yet, or unreadable — nothing to rotate
	}
	if info.Size()+incoming <= l.maxBytes {
		return
	}
	_ = os.Rename(l.path, l.path+".1") // best-effort; overwrites prior generation
}

// Path returns the on-disk audit log path.
func (l *AuditLog) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}
