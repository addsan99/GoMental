package gitsync

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritableEnsureCreatesInstanceBranch(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")

	m, err := NewWritable(WritableConfig{
		Remote:  remote,
		BaseRef: "main",
		Branch:  "gomental/test-machine/wiki",
		Dir:     dir,
	})
	if err != nil {
		t.Fatalf("NewWritable: %v", err)
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	branch := strings.TrimSpace(git(t, dir, "branch", "--show-current"))
	if branch != "gomental/test-machine/wiki" {
		t.Fatalf("branch = %q, want writable branch", branch)
	}
	if st := m.Status(); st.Branch != "gomental/test-machine/wiki" || st.BaseRef != "main" {
		t.Fatalf("status = %#v", st)
	}
}

func TestWritableEnsureFallsBackFromMainToMaster(t *testing.T) {
	requireGit(t)
	remote := makeRemoteWithBranch(t, "master")
	dir := filepath.Join(t.TempDir(), "clone")

	m, err := NewWritable(WritableConfig{
		Remote: remote,
		Branch: "gomental/test-machine/wiki",
		Dir:    dir,
	})
	if err != nil {
		t.Fatalf("NewWritable: %v", err)
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if st := m.Status(); st.BaseRef != "master" {
		t.Fatalf("BaseRef = %q, want master", st.BaseRef)
	}
}

func TestWritableStatusIgnoresMetadataDir(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")

	m, err := NewWritable(WritableConfig{
		Remote: remote,
		Branch: "gomental/test-machine/wiki",
		Dir:    dir,
	})
	if err != nil {
		t.Fatalf("NewWritable: %v", err)
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(dir, ".workspace", "graph.sqlite"), "derived\n")
	if _, err := m.CommitAndPush(context.Background(), "Ignore metadata", []string{".workspace/graph.sqlite"}); err != nil {
		t.Fatalf("CommitAndPush metadata-only: %v", err)
	}
	if st := m.Status(); st.Dirty {
		t.Fatalf("metadata-only changes should not make writable git status dirty: %#v", st)
	}
}

func TestWritableCommitAndPushStagesOnlyGivenPaths(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")

	m, err := NewWritable(WritableConfig{
		Remote:  remote,
		BaseRef: "main",
		Branch:  "gomental/test-machine/wiki",
		Dir:     dir,
	})
	if err != nil {
		t.Fatalf("NewWritable: %v", err)
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	writeFile(t, filepath.Join(dir, "alpha.md"), "# Alpha edited\n")
	writeFile(t, filepath.Join(dir, ".workspace", "graph.sqlite"), "derived\n")
	res, err := m.CommitAndPush(context.Background(), "Update alpha", []string{"alpha.md", ".workspace/graph.sqlite"})
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if !res.Committed || !res.Pushed {
		t.Fatalf("result = %#v, want committed+pushed", res)
	}

	check := filepath.Join(t.TempDir(), "check")
	git(t, "", "clone", "--branch", "gomental/test-machine/wiki", remote, check)
	data, err := os.ReadFile(filepath.Join(check, "alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(data), "\r\n", "\n") != "# Alpha edited\n" {
		t.Fatalf("alpha.md = %q", data)
	}
	if _, err := os.Stat(filepath.Join(check, ".workspace", "graph.sqlite")); !os.IsNotExist(err) {
		t.Fatalf(".workspace file should not be pushed, stat err=%v", err)
	}
}

func TestWritableOpenPullRequestPushesBranchFirst(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")

	var sawCreate bool
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/pulls"):
			return jsonResponse(200, `[]`), nil
		case req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/pulls"):
			data, _ := io.ReadAll(req.Body)
			body := string(data)
			if !strings.Contains(body, `"head":"acme:gomental/test-machine/wiki"`) {
				t.Fatalf("create PR payload = %s, want owner-qualified head", body)
			}
			sawCreate = true
			return jsonResponse(201, `{"html_url":"https://github.com/acme/wiki/pull/7","number":7}`), nil
		default:
			t.Fatalf("unexpected GitHub request: %s %s", req.Method, req.URL.String())
			return jsonResponse(500, `{}`), nil
		}
	})}

	m, err := NewWritable(WritableConfig{
		Remote:     remote,
		BaseRef:    "main",
		Branch:     "gomental/test-machine/wiki",
		Dir:        dir,
		Credential: Credential{Token: "test-token"},
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("NewWritable: %v", err)
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	m.cfg.Remote = "https://github.com/acme/wiki.git"
	m.status.Remote = m.cfg.Remote
	if _, err := m.OpenPullRequest(context.Background(), "Update", "Body"); err != nil {
		t.Fatalf("OpenPullRequest: %v", err)
	}
	if !sawCreate {
		t.Fatalf("expected create PR API call")
	}
	check := filepath.Join(t.TempDir(), "check")
	git(t, "", "clone", "--branch", "gomental/test-machine/wiki", remote, check)
}

func TestParseGitHubRepo(t *testing.T) {
	tests := map[string]githubRepo{
		"https://github.com/acme/wiki.git": {Owner: "acme", Name: "wiki"},
		"https://github.com/acme/wiki":     {Owner: "acme", Name: "wiki"},
		"git@github.com:acme/wiki.git":     {Owner: "acme", Name: "wiki"},
	}
	for remote, want := range tests {
		got, err := parseGitHubRepo(remote)
		if err != nil {
			t.Fatalf("parseGitHubRepo(%q): %v", remote, err)
		}
		if got != want {
			t.Fatalf("parseGitHubRepo(%q) = %#v, want %#v", remote, got, want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
