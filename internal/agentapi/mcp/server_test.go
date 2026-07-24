package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GoMental/internal/apphost"
	"GoMental/internal/workspace"
)

func testService(t *testing.T) *apphost.Host {
	t.Helper()
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\ntags: [go]\n---\n\n# Beta\n")
	store := workspace.NewRecentWorkspaceStore(filepath.Join(t.TempDir(), "recent.json"), 10)
	host, err := apphost.NewHost(apphost.Config{Environment: apphost.Headless(), RecentStore: store, StatePath: filepath.Join(t.TempDir(), "ui-state.json")})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	if _, err := host.OpenWorkspace(context.Background(), root); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
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

// run feeds the given JSON-RPC lines and returns the decoded responses.
func run(t *testing.T, srv *Server, lines ...string) []rpcResponse {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out strings.Builder
	if err := srv.Run(context.Background(), in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	var resps []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	return resps
}

// toolText extracts the text of a tools/call result.
func toolText(t *testing.T, resp rpcResponse) (string, bool) {
	t.Helper()
	data, _ := json.Marshal(resp.Result)
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	if len(res.Content) == 0 {
		return "", res.IsError
	}
	return res.Content[0].Text, res.IsError
}

func TestMCPInitializeAndToolsList(t *testing.T) {
	srv := NewServer(testService(t).Service())
	resps := run(t,
		srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	// initialize + tools/list => 2 responses (notification produces none).
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d: %#v", len(resps), resps)
	}
	// tools/list should include our core tools.
	data, _ := json.Marshal(resps[1].Result)
	var tl struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(data, &tl); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	want := map[string]bool{"search_wiki": false, "read_note": false, "list_notes": false, "create_note": false, "edit_note": false, "upload_asset": false, "backlinks": false, "neighborhood": false, "expand_context": false, "explain_link": false}
	for _, tool := range tl.Tools {
		want[tool.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("tools/list missing %q", name)
		}
	}
}

func TestMCPSearchReadCreateEdit(t *testing.T) {
	srv := NewServer(testService(t).Service())

	// search_wiki
	resps := run(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_wiki","arguments":{"query":"Alpha","limit":5}}}`)
	text, isErr := toolText(t, resps[0])
	if isErr || !strings.Contains(text, "alpha") {
		t.Fatalf("search_wiki unexpected: err=%v text=%s", isErr, text)
	}

	// create_note
	resps = run(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_note","arguments":{"id":"mcp/created","content":"---\ntype: concept\ntitle: MCP\n---\n\n# MCP\n","mode":"create"}}}`)
	text, isErr = toolText(t, resps[0])
	if isErr || !strings.Contains(text, "mcp/created") {
		t.Fatalf("create_note unexpected: err=%v text=%s", isErr, text)
	}

	// read_note to get version
	resps = run(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_note","arguments":{"id":"mcp/created"}}}`)
	text, _ = toolText(t, resps[0])
	var read struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal([]byte(text), &read)
	if read.Version == "" {
		t.Fatalf("read_note returned no version: %s", text)
	}

	// edit_note with the version
	call := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"edit_note","arguments":{"id":"mcp/created","content":"---\ntype: concept\ntitle: MCP\n---\n\n# MCP edited longer\n","base_version":"` + read.Version + `"}}}`
	resps = run(t, srv, call)
	text, isErr = toolText(t, resps[0])
	if isErr {
		t.Fatalf("edit_note failed: %s", text)
	}

	// edit_note with a stale version -> conflict (isError)
	stale := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"edit_note","arguments":{"id":"mcp/created","content":"x","base_version":"` + read.Version + `"}}}`
	resps = run(t, srv, stale)
	text, isErr = toolText(t, resps[0])
	if !isErr || !strings.Contains(strings.ToLower(text), "conflict") {
		t.Fatalf("expected conflict error, got err=%v text=%s", isErr, text)
	}
}

func TestMCPUploadAsset(t *testing.T) {
	srv := NewServer(testService(t).Service())

	// A 1x1 transparent PNG, base64-encoded.
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"upload_asset","arguments":{"id":"alpha","file_name":"pixel.png","mime_type":"image/png","data_base64":"` + png + `"}}}`
	text, isErr := toolText(t, run(t, srv, call)[0])
	if isErr {
		t.Fatalf("upload_asset failed: %s", text)
	}
	var res struct {
		Path     string `json:"path"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("decode upload_asset: %v (%s)", err, text)
	}
	if res.Path == "" || !strings.Contains(res.Markdown, "![") || !strings.Contains(res.Markdown, ".png") {
		t.Fatalf("upload_asset returned unexpected result: %#v", res)
	}

	// Bad base64 -> tool error, not a panic.
	bad := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"upload_asset","arguments":{"id":"alpha","file_name":"x.png","mime_type":"image/png","data_base64":"!!!notbase64!!!"}}}`
	if _, isErr := toolText(t, run(t, srv, bad)[0]); !isErr {
		t.Fatal("expected error for invalid base64 upload")
	}
}

func TestMCPListNotesTypeFilter(t *testing.T) {
	srv := NewServer(testService(t).Service())

	// The fixture seeds two type:concept notes; add one with a different type so the
	// filter has something to exclude. Taxonomy-agnostic: any string type works.
	create := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_note","arguments":{"id":"gamma","content":"---\ntype: procedure\ntitle: Gamma\n---\n\n# Gamma\n","mode":"upsert"}}}`
	if _, isErr := toolText(t, run(t, srv, create)[0]); isErr {
		t.Fatalf("create_note failed")
	}

	// list_notes with type=procedure returns only gamma (case-insensitive).
	resps := run(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_notes","arguments":{"type":"PROCEDURE"}}}`)
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("list_notes failed: %s", text)
	}
	var result struct {
		Notes []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"notes"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode list_notes: %v (%s)", err, text)
	}
	if result.Count != 1 || len(result.Notes) != 1 || result.Notes[0].ID != "gamma" || result.Notes[0].Type != "procedure" {
		t.Fatalf("expected only gamma for type=procedure, got %#v", result)
	}

	// Without the filter, all notes come back and each carries its type.
	resps = run(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_notes","arguments":{}}}`)
	text, _ = toolText(t, resps[0])
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode list_notes (all): %v (%s)", err, text)
	}
	if result.Count < 3 {
		t.Fatalf("expected at least 3 notes unfiltered, got %#v", result)
	}
	for _, n := range result.Notes {
		if n.Type == "" {
			t.Fatalf("expected every note to carry a type, got %#v", result.Notes)
		}
	}
}

func TestMCPExpandContextAndExplainLink(t *testing.T) {
	srv := NewServer(testService(t).Service())

	// expand_context on alpha (which links to beta) should return beta as a neighbor.
	resps := run(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"expand_context","arguments":{"id":"alpha","depth":1}}}`)
	text, isErr := toolText(t, resps[0])
	if isErr || !strings.Contains(text, "beta") || !strings.Contains(text, "neighbors") {
		t.Fatalf("expand_context unexpected: err=%v text=%s", isErr, text)
	}

	// explain_link alpha->beta: hard link + shared tag "go" + title mention.
	resps = run(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"explain_link","arguments":{"source":"alpha","target":"beta"}}}`)
	text, isErr = toolText(t, resps[0])
	if isErr {
		t.Fatalf("explain_link failed: %s", text)
	}
	var expl struct {
		Related  bool `json:"related"`
		HardLink bool `json:"hardLink"`
		Evidence []struct {
			Kind string `json:"kind"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(text), &expl); err != nil {
		t.Fatalf("decode explain_link: %v (%s)", err, text)
	}
	if !expl.Related || !expl.HardLink || len(expl.Evidence) == 0 {
		t.Fatalf("explain_link unexpected: %s", text)
	}
}

func TestMCPUnknownToolAndMethod(t *testing.T) {
	srv := NewServer(testService(t).Service())
	resps := run(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"does/not/exist"}`,
	)
	if resps[0].Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resps[1].Error == nil || resps[1].Error.Code != codeMethodNotFound {
		t.Fatalf("expected method-not-found, got %#v", resps[1].Error)
	}
}
