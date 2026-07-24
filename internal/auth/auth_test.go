package auth

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRoleHierarchy(t *testing.T) {
	admin := Actor{Role: RoleAdmin}
	editor := Actor{Role: RoleEditor}
	viewer := Actor{Role: RoleViewer}

	if !admin.Can(RoleAdmin) || !admin.Can(RoleEditor) || !admin.Can(RoleViewer) {
		t.Fatal("admin should satisfy all roles")
	}
	if editor.Can(RoleAdmin) {
		t.Fatal("editor must not satisfy admin")
	}
	if !editor.Can(RoleEditor) || !editor.Can(RoleViewer) {
		t.Fatal("editor should satisfy editor+viewer")
	}
	if viewer.Can(RoleEditor) || viewer.Can(RoleAdmin) {
		t.Fatal("viewer must not satisfy editor/admin")
	}
}

func TestTrustAllNeverRejects(t *testing.T) {
	a := NewTrustAll()
	if a.Enforced() {
		t.Fatal("trust-all must not report enforced")
	}
	actor, err := a.Authenticate(httptest.NewRequest("GET", "/api/notes", nil))
	if err != nil {
		t.Fatalf("trust-all authenticate: %v", err)
	}
	if actor.Role != RoleAdmin {
		t.Fatalf("trust-all default actor should be admin, got %q", actor.Role)
	}
}

func TestAuditLogAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "audit.log")
	log, err := OpenAuditLog(path)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	if err := log.Record(AuditEntry{Actor: "local", Role: "admin", Action: "note.save", NoteID: "alpha", Version: "123-45", Result: "ok"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := log.Record(AuditEntry{Actor: "local", Action: "note.delete", NoteID: "beta", Result: "ok"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer f.Close()
	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("decode entry: %v", err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}
	if entries[0].NoteID != "alpha" || entries[0].Version != "123-45" || entries[0].Time == "" {
		t.Fatalf("unexpected first entry: %#v", entries[0])
	}
}

// TestNilAuditLogIsNoop confirms callers can hold a nil *AuditLog.
func TestNilAuditLogIsNoop(t *testing.T) {
	var l *AuditLog
	if err := l.Record(AuditEntry{Action: "x"}); err != nil {
		t.Fatalf("nil audit record should be no-op, got %v", err)
	}
}
