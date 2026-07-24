package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/apphost"
	"GoMental/internal/auth"
	"GoMental/internal/gitsync"
	"GoMental/internal/serverconfig"
	"GoMental/internal/workspace"
)

type fakeGitManager struct {
	status    gitsync.Status
	result    gitsync.Result
	syncErr   error
	syncCalls int
}

func (f *fakeGitManager) Sync(ctx context.Context) (gitsync.Result, error) {
	f.syncCalls++
	return f.result, f.syncErr
}

func (f *fakeGitManager) Status() gitsync.Status { return f.status }

// newGitServer builds a real server over a temp workspace with the given
// read-only flag and (optional) git manager.
func newGitServer(t *testing.T, readOnly bool, mgr GitManager, webhookSecret string) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n")

	store := workspace.NewRecentWorkspaceStore(filepath.Join(t.TempDir(), "recent.json"), 10)
	host, err := apphost.NewHost(apphost.Config{
		Environment: apphost.Headless(),
		RecentStore: store,
		StatePath:   filepath.Join(t.TempDir(), "ui-state.json"),
	})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	if _, err := host.OpenWorkspace(context.Background(), root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	srv := NewServer(Options{
		Host:             host,
		Config:           serverconfig.Config{Addr: ":0", WorkspaceRoot: root},
		ReadOnly:         readOnly,
		GitManager:       mgr,
		GitWebhookSecret: webhookSecret,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = host.Close()
	})
	return ts, root
}

func TestReadOnlyRejectsContentWrites(t *testing.T) {
	ts, _ := newGitServer(t, true, nil, "")

	// Create a note -> 403 workspace.read_only.
	body := bytes.NewBufferString(`{"title":"New","content":"# New\n"}`)
	resp, err := http.Post(ts.URL+"/api/notes", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	var payload struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload.Code != "workspace.read_only" {
		t.Fatalf("expected code workspace.read_only, got %q", payload.Code)
	}

	// Reads still work.
	rd, err := http.Get(ts.URL + "/api/notes")
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Body.Close()
	if rd.StatusCode != http.StatusOK {
		t.Fatalf("read should be 200, got %d", rd.StatusCode)
	}
}

func TestGitEndpointsDisabledWhenNoManager(t *testing.T) {
	ts, _ := newGitServer(t, false, nil, "")
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/git/status"},
		{http.MethodPost, "/api/git/sync"},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404 when git disabled, got %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestGitStatusAndSyncEndpoints(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	mgr := &fakeGitManager{
		status: gitsync.Status{Remote: "https://x/w.git", Ref: "main", Commit: "abc123def456", LastSyncAt: &now},
		result: gitsync.Result{OldCommit: "old", NewCommit: "new", Changed: []string{"a.md", "b.md"}, Deleted: []string{"c.md"}},
	}
	ts, _ := newGitServer(t, true, mgr, "")

	// Status reflects the manager, with SPA-facing field names.
	resp, err := http.Get(ts.URL + "/api/git/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: expected 200, got %d", resp.StatusCode)
	}
	var st map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st["ref"] != "main" || st["commit"] != "abc123def456" || st["remote"] != "https://x/w.git" {
		t.Fatalf("unexpected status body: %#v", st)
	}
	if st["lastSyncAt"] == nil {
		t.Fatal("expected lastSyncAt to be set")
	}

	// Sync (trust-all actor is admin) triggers the manager and reports counts.
	sresp, err := http.Post(ts.URL+"/api/git/sync", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("sync: expected 200, got %d", sresp.StatusCode)
	}
	if mgr.syncCalls != 1 {
		t.Fatalf("expected manager.Sync called once, got %d", mgr.syncCalls)
	}
	var sbody map[string]any
	if err := json.NewDecoder(sresp.Body).Decode(&sbody); err != nil {
		t.Fatal(err)
	}
	if sbody["ok"] != true || sbody["changed"].(float64) != 2 || sbody["deleted"].(float64) != 1 {
		t.Fatalf("unexpected sync body: %#v", sbody)
	}
}

// authorizeGitSync: admin passes unconditionally; a non-admin needs a matching
// webhook token via header or query.
func TestAuthorizeGitSync(t *testing.T) {
	s := &Server{gitWebhookSecret: "s3cret"}
	viewer := auth.Actor{ID: "v", Role: auth.RoleViewer}
	admin := auth.LocalActor

	withActor := func(a auth.Actor, url string, header string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, url, nil)
		if header != "" {
			r.Header.Set("X-GoMental-Token", header)
		}
		return r.WithContext(context.WithValue(r.Context(), actorContextKey, a))
	}

	if !s.authorizeGitSync(withActor(admin, "/api/git/sync", "")) {
		t.Fatal("admin should be authorized without a token")
	}
	if s.authorizeGitSync(withActor(viewer, "/api/git/sync", "")) {
		t.Fatal("viewer without a token must be rejected")
	}
	if s.authorizeGitSync(withActor(viewer, "/api/git/sync", "wrong")) {
		t.Fatal("viewer with wrong token must be rejected")
	}
	if !s.authorizeGitSync(withActor(viewer, "/api/git/sync", "s3cret")) {
		t.Fatal("viewer with correct header token should be authorized")
	}
	if !s.authorizeGitSync(withActor(viewer, "/api/git/sync?token=s3cret", "")) {
		t.Fatal("viewer with correct query token should be authorized")
	}

	// No secret configured: only admins can sync.
	noSecret := &Server{}
	if noSecret.authorizeGitSync(withActor(viewer, "/api/git/sync?token=whatever", "")) {
		t.Fatal("with no secret configured a viewer must never be authorized")
	}
}
