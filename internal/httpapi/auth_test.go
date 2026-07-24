package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"GoMental/internal/apphost"
	"GoMental/internal/auth"
	"GoMental/internal/serverconfig"
	"GoMental/internal/workspace"
)

func newAuthTestServer(t *testing.T, authr auth.Authenticator) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n")
	store := workspace.NewRecentWorkspaceStore(filepath.Join(t.TempDir(), "recent.json"), 10)
	host, err := apphost.NewHost(apphost.Config{Environment: apphost.Headless(), RecentStore: store, StatePath: filepath.Join(t.TempDir(), "ui-state.json")})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	if _, err := host.OpenWorkspace(context.Background(), root); err != nil {
		t.Fatalf("open: %v", err)
	}
	auditPath := filepath.Join(root, ".workspace", "audit", "audit.log")
	auditLog, err := auth.OpenAuditLog(auditPath)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	srv := NewServer(Options{Host: host, Config: serverconfig.Config{Addr: ":0", WorkspaceRoot: root}, Auth: authr, Audit: auditLog})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = host.Close()
	})
	return ts, auditPath
}

// TestAuditTrailRecordsWrites is the Phase 20 acceptance criterion (trust-all):
// every write appears in the audit log with the acting user and note version.
func TestAuditTrailRecordsWrites(t *testing.T) {
	ts, auditPath := newAuthTestServer(t, auth.NewTrustAll())
	client := ts.Client()

	body, _ := json.Marshal(map[string]string{"content": "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha edited\n"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/notes/alpha", bytes.NewReader(body))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	entries := readAudit(t, auditPath)
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	last := entries[len(entries)-1]
	if last.Action != "note.save" || last.NoteID != "alpha" || last.Result != "ok" {
		t.Fatalf("unexpected audit entry: %#v", last)
	}
	if last.Actor != "local" || last.Version == "" {
		t.Fatalf("audit entry missing actor/version: %#v", last)
	}
}

// TestEnforcedAuthRejects proves the enforcement mechanism is live even though
// trust-all is the default: a restrictive authenticator yields 401 (no identity)
// and a viewer-role actor is 403 on a write.
func TestEnforcedAuthRejects(t *testing.T) {
	// Unauthenticated -> 401.
	tsUnauthed, _ := newAuthTestServer(t, stubAuth{err: auth.ErrUnauthorized})
	resp, err := tsUnauthed.Client().Get(tsUnauthed.URL + "/api/notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Viewer role -> 403 on a write, 200 on a read.
	tsViewer, _ := newAuthTestServer(t, stubAuth{actor: auth.Actor{ID: "v", Role: auth.RoleViewer}})
	client := tsViewer.Client()

	readResp, err := client.Get(tsViewer.URL + "/api/notes")
	if err != nil {
		t.Fatalf("viewer read: %v", err)
	}
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("viewer read should be 200, got %d", readResp.StatusCode)
	}
	readResp.Body.Close()

	body, _ := json.Marshal(map[string]string{"content": "x"})
	req, _ := http.NewRequest(http.MethodPut, tsViewer.URL+"/api/notes/alpha", bytes.NewReader(body))
	writeResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("viewer write: %v", err)
	}
	defer writeResp.Body.Close()
	if writeResp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer write should be 403, got %d", writeResp.StatusCode)
	}
}

type stubAuth struct {
	actor auth.Actor
	err   error
}

func (s stubAuth) Authenticate(*http.Request) (auth.Actor, error) {
	if s.err != nil {
		return auth.Actor{}, s.err
	}
	return s.actor, nil
}
func (s stubAuth) Enforced() bool { return true }

func readAudit(t *testing.T, path string) []auth.AuditEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	defer f.Close()
	var out []auth.AuditEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e auth.AuditEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("decode audit: %v", err)
		}
		out = append(out, e)
	}
	return out
}
