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
	"strings"
	"testing"
	"time"

	"GoMental/internal/apphost"
	"GoMental/internal/serverconfig"
	"GoMental/internal/workspace"
)

func newTestServer(t *testing.T) (*httptest.Server, *apphost.Host, string) {
	t.Helper()
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\ntags: [go]\n---\n\n# Beta\n")

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
	srv := NewServer(Options{Host: host, Config: serverconfig.Config{Addr: ":0", WorkspaceRoot: root}})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = host.Close()
	})
	return ts, host, root
}

func writeNote(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPListReadSaveSearchDelete(t *testing.T) {
	ts, _, _ := newTestServer(t)
	client := ts.Client()

	// List
	resp, err := client.Get(ts.URL + "/api/notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var notes []map[string]any
	decode(t, resp, &notes)
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}

	// Read (nested id path-value semantics via {id...})
	resp, err = client.Get(ts.URL + "/api/notes/alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if etag := resp.Header.Get("ETag"); etag == "" {
		t.Fatal("expected ETag header on read")
	}
	var note map[string]any
	decode(t, resp, &note)
	if note["id"] != "alpha" || note["content"] == "" {
		t.Fatalf("unexpected note: %#v", note)
	}

	// Save
	body, _ := json.Marshal(map[string]string{"content": "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha updated\n"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/notes/alpha", bytes.NewReader(body))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Search
	body, _ = json.Marshal(map[string]any{"text": "Alpha", "limit": 10})
	resp, err = client.Post(ts.URL+"/api/search", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var results []map[string]any
	decode(t, resp, &results)
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}

	// Delete
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/notes/beta", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHTTPReadMissingNoteIsNotFound(t *testing.T) {
	ts, _, _ := newTestServer(t)
	resp, err := ts.Client().Get(ts.URL + "/api/notes/does-not-exist")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing note, got %d", resp.StatusCode)
	}
}

func TestHTTPOpenForeignWorkspaceForbidden(t *testing.T) {
	ts, _, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"root": "C:/some/other/place"})
	resp, err := ts.Client().Post(ts.URL+"/api/workspace/open", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign workspace, got %d", resp.StatusCode)
	}
}

// TestHTTPSaveTriggersSSEEvent verifies the acceptance criterion: a PUT save
// produces a note:updated SSE event observed by a separately-connected client.
func TestHTTPSaveTriggersSSEEvent(t *testing.T) {
	ts, _, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status: %d", resp.StatusCode)
	}

	observed := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				name := strings.TrimPrefix(line, "event: ")
				if name == "note:updated" {
					select {
					case observed <- name:
					default:
					}
					return
				}
			}
		}
	}()

	// Give the SSE subscription a moment to register on the hub.
	time.Sleep(200 * time.Millisecond)

	body, _ := json.Marshal(map[string]string{"content": "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha SSE\n"})
	putReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/notes/alpha", bytes.NewReader(body))
	putResp, err := ts.Client().Do(putReq)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	putResp.Body.Close()

	select {
	case <-observed:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("did not observe note:updated SSE event")
	}
}

// TestHTTPIfMatchConflict verifies optimistic concurrency over HTTP: a stale
// If-Match yields 412 Precondition Failed with the current version in ETag.
func TestHTTPIfMatchConflict(t *testing.T) {
	ts, _, _ := newTestServer(t)
	client := ts.Client()

	// Read to obtain the current ETag (version token).
	resp, err := client.Get(ts.URL + "/api/notes/alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	etag := resp.Header.Get("ETag")
	resp.Body.Close()
	if etag == "" {
		t.Fatal("expected ETag on read")
	}

	put := func(content, ifMatch string) *http.Response {
		body, _ := json.Marshal(map[string]string{"content": content})
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/notes/alpha", bytes.NewReader(body))
		req.Header.Set("If-Match", ifMatch)
		r, err := client.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		return r
	}

	// First conditional PUT with the fresh ETag succeeds.
	r1 := put("---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha via If-Match longer body\n", etag)
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first conditional PUT status: %d", r1.StatusCode)
	}
	r1.Body.Close()

	// Second PUT reusing the now-stale ETag is a 412.
	r2 := put("---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha stale\n", etag)
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for stale If-Match, got %d", r2.StatusCode)
	}
	if r2.Header.Get("ETag") == "" {
		t.Fatal("expected current-version ETag on 412 response")
	}
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
