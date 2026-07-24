package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"GoMental/internal/apphost"
	"GoMental/internal/auth"
	"GoMental/internal/serverconfig"
	"GoMental/internal/workspace"
)

func newAgentTestServer(t *testing.T) (*httptest.Server, *auth.APIKeyStore) {
	t.Helper()
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\n")
	store := workspace.NewRecentWorkspaceStore(filepath.Join(t.TempDir(), "recent.json"), 10)
	host, err := apphost.NewHost(apphost.Config{Environment: apphost.Headless(), RecentStore: store, StatePath: filepath.Join(t.TempDir(), "ui-state.json")})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	if _, err := host.OpenWorkspace(context.Background(), root); err != nil {
		t.Fatalf("open: %v", err)
	}
	keyStore, err := auth.OpenAPIKeyStore(filepath.Join(root, ".workspace", "server", "api-keys.json"))
	if err != nil {
		t.Fatalf("keystore: %v", err)
	}
	authr := auth.BearerAuthenticator{Store: keyStore, Fallback: auth.LocalActor}
	srv := NewServer(Options{Host: host, Config: serverconfig.Config{Addr: ":0", WorkspaceRoot: root}, Auth: authr, KeyStore: keyStore})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = host.Close()
	})
	return ts, keyStore
}

// TestAgentWorkflowWithAPIKey is the Phase 22 acceptance criterion: an agent
// with an API key can search → read a hit → edit with the returned version →
// create a new note, all over HTTP with no browser session; and a revoked key
// is rejected.
func TestAgentWorkflowWithAPIKey(t *testing.T) {
	ts, keyStore := newAgentTestServer(t)
	client := ts.Client()

	// Mint an editor key directly (equivalent to POST /api/keys under admin).
	token, rec, err := keyStore.Create("agent-1", auth.RoleEditor)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	do := func(method, path string, body any) *http.Response {
		var reader *bytes.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			reader = bytes.NewReader(data)
		} else {
			reader = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, ts.URL+path, reader)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}

	// search
	resp := do(http.MethodPost, "/api/search", map[string]any{"text": "Alpha", "limit": 10})
	var results []map[string]any
	decode(t, resp, &results)
	if len(results) == 0 {
		t.Fatal("agent search returned nothing")
	}
	hitID, _ := results[0]["id"].(string)

	// read the hit (obtain version)
	resp = do(http.MethodGet, "/api/notes/"+hitID, nil)
	var note map[string]any
	decode(t, resp, &note)
	version, _ := note["version"].(string)
	if version == "" {
		t.Fatal("read did not return a version")
	}

	// edit with the returned version
	resp = do(http.MethodPut, "/api/notes/"+hitID, map[string]any{"content": "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha edited by agent longer\n", "baseVersion": version})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent edit status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// create a new note (idempotent create)
	resp = do(http.MethodPost, "/api/notes", map[string]any{"id": "agent/created", "content": "---\ntype: concept\ntitle: Created\n---\n\n# Created\n", "mode": "create"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("agent create status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// create-or-fail collision -> 409
	resp = do(http.MethodPost, "/api/notes", map[string]any{"id": "agent/created", "content": "x", "mode": "create"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate create, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// revoke the key -> subsequent request rejected
	if ok, err := keyStore.Revoke(rec.ID); err != nil || !ok {
		t.Fatalf("revoke: ok=%v err=%v", ok, err)
	}
	resp = do(http.MethodGet, "/api/notes", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked key, got %d", resp.StatusCode)
	}
}

func TestOpenAPISpecServed(t *testing.T) {
	ts, _ := newAgentTestServer(t)
	resp, err := ts.Client().Get(ts.URL + "/api/openapi.json")
	if err != nil {
		t.Fatalf("openapi: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi status: %d", resp.StatusCode)
	}
	var spec map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		t.Fatalf("openapi not valid JSON: %v", err)
	}
	if spec["openapi"] == nil || spec["paths"] == nil {
		t.Fatalf("openapi spec missing core fields: %#v", spec)
	}
}
