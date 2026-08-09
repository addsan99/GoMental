package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestSyncClonesAndFetchesWithPureGoGit(t *testing.T) {
	remotePath := filepath.Join(t.TempDir(), "remote")
	remote, err := git.PlainInit(remotePath, false)
	if err != nil {
		t.Fatalf("init remote: %v", err)
	}
	commitTestNote(t, remote, remotePath, "alpha.md", testOKF("Alpha", "Initial mobile content"))

	root := t.TempDir()
	configJSON, err := json.Marshal(config{
		RepositoryPath: filepath.Join(root, "repository"),
		DataPath:       filepath.Join(root, "data"),
	})
	if err != nil {
		t.Fatal(err)
	}
	core, err := Open(string(configJSON))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = core.Close() })

	requestJSON, err := json.Marshal(syncRequest{Remote: remotePath, Ref: "master"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := core.Sync(string(requestJSON))
	if err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	if !strings.Contains(first, `"cloned":true`) || !strings.Contains(first, `"noteCount":1`) {
		t.Fatalf("unexpected initial result: %s", first)
	}
	writeTestFile(t, filepath.Join(root, "repository", "rogue.md"), testOKF("Rogue", "not tracked"))
	if err := core.Rebuild(); err != nil {
		t.Fatalf("Rebuild with untracked note: %v", err)
	}
	listed, err := core.ListNotes("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, `"id":"rogue"`) {
		t.Fatalf("untracked note was exposed: %s", listed)
	}

	commitTestNote(t, remote, remotePath, "beta.md", testOKF("Beta", "Fetched Android update"))
	second, err := core.Sync(string(requestJSON))
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if !strings.Contains(second, `"fetched":true`) || !strings.Contains(second, `"changed":true`) || !strings.Contains(second, `"noteCount":2`) {
		t.Fatalf("unexpected fetch result: %s", second)
	}
	searched, err := core.Search(`{"text":"Android","limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searched, `"id":"beta"`) {
		t.Fatalf("fetched note was not indexed: %s", searched)
	}
}

func TestSyncRejectsBranchChangeInExistingSession(t *testing.T) {
	remotePath := filepath.Join(t.TempDir(), "remote")
	remote, err := git.PlainInit(remotePath, false)
	if err != nil {
		t.Fatal(err)
	}
	commitTestNote(t, remote, remotePath, "alpha.md", testOKF("Alpha", "body"))
	root := t.TempDir()
	configJSON, _ := json.Marshal(config{RepositoryPath: filepath.Join(root, "repository"), DataPath: filepath.Join(root, "data")})
	core, err := Open(string(configJSON))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	initial, _ := json.Marshal(syncRequest{Remote: remotePath, Ref: "master"})
	if _, err := core.Sync(string(initial)); err != nil {
		t.Fatal(err)
	}
	changed, _ := json.Marshal(syncRequest{Remote: remotePath, Ref: "other"})
	if _, err := core.Sync(string(changed)); err == nil || !strings.Contains(err.Error(), "new repository session") {
		t.Fatalf("expected branch change rejection, got %v", err)
	}
}

func TestOpenRepairsTrackedWorktreeBeforeIndexing(t *testing.T) {
	remotePath := filepath.Join(t.TempDir(), "remote")
	remote, err := git.PlainInit(remotePath, false)
	if err != nil {
		t.Fatal(err)
	}
	commitTestNote(t, remote, remotePath, "alpha.md", testOKF("Alpha", "committed content"))
	root := t.TempDir()
	cfg := config{RepositoryPath: filepath.Join(root, "repository"), DataPath: filepath.Join(root, "data")}
	configJSON, _ := json.Marshal(cfg)
	core, err := Open(string(configJSON))
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, _ := json.Marshal(syncRequest{Remote: remotePath, Ref: "master"})
	if _, err := core.Sync(string(requestJSON)); err != nil {
		t.Fatal(err)
	}
	if err := core.Close(); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.RepositoryPath, "alpha.md"), testOKF("Alpha", "partial reset content"))

	reopened, err := Open(string(configJSON))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	detail, err := reopened.ReadNote("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "committed content") || strings.Contains(detail, "partial reset content") {
		t.Fatalf("worktree was not repaired from HEAD: %s", detail)
	}
}

func TestSyncRejectsInsecureAndCredentialBearingRemotes(t *testing.T) {
	for _, remote := range []string{"http://example.com/notes.git", "https://token@example.com/notes.git"} {
		if err := validateRemote(remote); err == nil {
			t.Fatalf("validateRemote(%q) succeeded", remote)
		}
	}
}

func commitTestNote(t *testing.T, repository *git.Repository, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	_, err = worktree.Commit("update "+name, &git.CommitOptions{Author: &object.Signature{
		Name: "GoMental Test", Email: "test@gomental.local", When: time.Now(),
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func testOKF(title, body string) string {
	return "---\ntype: general\ntitle: " + title + "\n---\n\n# " + title + "\n\n" + body + "\n"
}
