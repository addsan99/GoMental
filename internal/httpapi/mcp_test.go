package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"GoMental/internal/apphost"
	"GoMental/internal/auth"
	"GoMental/internal/serverconfig"
	"GoMental/internal/workspace"
)

// newAgentTestServerEnforced is newAgentTestServer with API-key identity actually
// enforced (RequireKey), so role differences are observable — a keyless or
// wrong-role request is rejected rather than promoted to the trust-all admin.
func newAgentTestServerEnforced(t *testing.T) (*httptest.Server, *auth.APIKeyStore) {
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
	authr := auth.BearerAuthenticator{Store: keyStore, Fallback: auth.LocalActor, RequireKey: true}
	srv := NewServer(Options{Host: host, Config: serverconfig.Config{Addr: ":0", WorkspaceRoot: root}, Auth: authr, KeyStore: keyStore})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = host.Close()
	})
	return ts, keyStore
}

// mcpRoundTrip POSTs one JSON-RPC message to /mcp with the given bearer token
// (empty = no auth header) and returns the decoded response envelope.
func mcpRoundTrip(t *testing.T, url, token, payload string) (status int, env map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/mcp", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode /mcp response (status %d): %v", resp.StatusCode, err)
	}
	return resp.StatusCode, env
}

// toolCallText pulls the text payload out of a tools/call result envelope.
func toolCallText(t *testing.T, env map[string]any) (text string, isError bool) {
	t.Helper()
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %#v", env)
	}
	isError, _ = result["isError"].(bool)
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result has no content: %#v", result)
	}
	first, _ := content[0].(map[string]any)
	text, _ = first["text"].(string)
	return text, isError
}

// TestMCPOverHTTP exercises the Streamable-HTTP MCP endpoint end to end:
// initialize, tools/list, a read tool, and a write tool — the Phase 23 mode (b)
// acceptance path for a remote agent reaching the central Service over HTTP.
func TestMCPOverHTTP(t *testing.T) {
	ts, _ := newAgentTestServer(t)

	// initialize
	status, env := mcpRoundTrip(t, ts.URL, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if status != http.StatusOK {
		t.Fatalf("initialize status: %d", status)
	}
	res, _ := env["result"].(map[string]any)
	if res["protocolVersion"] == nil || res["serverInfo"] == nil {
		t.Fatalf("initialize missing fields: %#v", env)
	}

	// tools/list should advertise the tool surface.
	_, env = mcpRoundTrip(t, ts.URL, "", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	res, _ = env["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("tools/list returned no tools: %#v", env)
	}

	// search_wiki (read) should find the seeded note.
	_, env = mcpRoundTrip(t, ts.URL, "",
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_wiki","arguments":{"query":"Alpha"}}}`)
	text, isErr := toolCallText(t, env)
	if isErr || !strings.Contains(text, "alpha") {
		t.Fatalf("search_wiki result unexpected (isError=%v): %s", isErr, text)
	}

	// create_note (write) should succeed under the trust-all admin actor.
	_, env = mcpRoundTrip(t, ts.URL, "",
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_note","arguments":{"id":"mcp/made","content":"---\ntype: concept\ntitle: Made\n---\n\n# Made\n","mode":"create"}}}`)
	if _, isErr := toolCallText(t, env); isErr {
		t.Fatalf("create_note reported error: %#v", env)
	}
}

// TestMCPNotificationYields202 verifies a notification (no id) gets an empty 202.
func TestMCPNotificationYields202(t *testing.T) {
	ts, _ := newAgentTestServer(t)
	status, _ := mcpRoundTrip(t, ts.URL, "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status != http.StatusAccepted {
		t.Fatalf("notification status: got %d want 202", status)
	}
}

// TestMCPWriteRequiresEditor confirms a viewer key can read but not write over
// MCP, while an editor key can — the per-tool authorization that keeps agent
// writes governed the same way as the REST surface.
func TestMCPWriteRequiresEditor(t *testing.T) {
	ts, keyStore := newAgentTestServerEnforced(t)

	viewerTok, _, err := keyStore.Create("viewer-agent", auth.RoleViewer)
	if err != nil {
		t.Fatalf("create viewer key: %v", err)
	}
	editorTok, _, err := keyStore.Create("editor-agent", auth.RoleEditor)
	if err != nil {
		t.Fatalf("create editor key: %v", err)
	}

	// Viewer can read.
	_, env := mcpRoundTrip(t, ts.URL, viewerTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_wiki","arguments":{"query":"Alpha"}}}`)
	if _, isErr := toolCallText(t, env); isErr {
		t.Fatalf("viewer search should succeed: %#v", env)
	}

	// Viewer cannot write: JSON-RPC error with the role code, and no note created.
	_, env = mcpRoundTrip(t, ts.URL, viewerTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_note","arguments":{"id":"viewer/denied","content":"x","mode":"create"}}}`)
	rpcErr, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("viewer write should be rejected with an error, got: %#v", env)
	}
	if code, _ := rpcErr["code"].(float64); int(code) != mcpErrRole {
		t.Fatalf("viewer write error code: got %v want %d", rpcErr["code"], mcpErrRole)
	}

	// Editor can write.
	_, env = mcpRoundTrip(t, ts.URL, editorTok,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_note","arguments":{"id":"editor/made","content":"---\ntype: concept\ntitle: Made\n---\n\n# Made\n","mode":"create"}}}`)
	if _, isErr := toolCallText(t, env); isErr {
		t.Fatalf("editor write should succeed: %#v", env)
	}
}

// TestMCPGetDeclinesStream verifies GET /mcp returns 405 (no server-initiated stream).
func TestMCPGetDeclinesStream(t *testing.T) {
	ts, _ := newAgentTestServer(t)
	resp, err := ts.Client().Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp status: got %d want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "POST" {
		t.Fatalf("GET /mcp Allow header: got %q want POST", allow)
	}
}
