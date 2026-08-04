package gitsync

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// git runs a git command in dir with a deterministic identity so commits work
// in a bare CI environment. It fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.email=test@gomental.local",
		"-c", "user.name=GoMental Test",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", full...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s) failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// makeRemote builds a bare "remote" repo with one seeded commit and returns its
// path. The seed commit contains alpha.md and beta.md.
func makeRemote(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "init")
	git(t, seed, "checkout", "-B", "main")
	writeFile(t, filepath.Join(seed, "alpha.md"), "# Alpha\n")
	writeFile(t, filepath.Join(seed, "beta.md"), "# Beta\n")
	git(t, seed, "add", "-A")
	git(t, seed, "commit", "-m", "seed")

	bare := filepath.Join(root, "remote.git")
	git(t, "", "clone", "--bare", "--branch", "main", seed, bare)
	return bare
}

func makeRemoteWithBranch(t *testing.T, branch string) string {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "init")
	git(t, seed, "checkout", "-B", branch)
	writeFile(t, filepath.Join(seed, "alpha.md"), "# Alpha\n")
	git(t, seed, "add", "-A")
	git(t, seed, "commit", "-m", "seed")

	bare := filepath.Join(root, "remote.git")
	git(t, "", "clone", "--bare", "--branch", branch, seed, bare)
	return bare
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH; skipping gitsync integration tests")
	}
}

func newManager(t *testing.T, remote, dir string) *Manager {
	t.Helper()
	m, err := New(Config{Remote: remote, Ref: "main", Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
}

func TestEnsureClonesFreshDir(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")

	m := newManager(t, remote, dir)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("expected .git in cloned dir: %v", err)
	}
	remoteHead := strings.TrimSpace(git(t, remote, "rev-parse", "HEAD"))
	localHead := headSHA(t, dir)
	if remoteHead != localHead {
		t.Fatalf("HEAD mismatch after clone: remote=%s local=%s", remoteHead, localHead)
	}
	if got := m.Status().Commit; got != short(localHead) {
		t.Fatalf("Status().Commit = %q, want %q", got, short(localHead))
	}
}

func TestEnsureAdoptsExistingRepo(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")
	git(t, "", "clone", "--branch", "main", remote, dir)

	m := newManager(t, remote, dir)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure on existing repo: %v", err)
	}
	if m.Status().Commit == "" {
		t.Fatalf("expected Commit populated after adopting existing repo")
	}
}

func TestEnsureRejectsNonEmptyNonRepo(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "stray.txt"), "not a repo\n")

	m := newManager(t, remote, dir)
	err := m.Ensure(context.Background())
	if err == nil {
		t.Fatalf("expected error cloning into non-empty non-repo dir")
	}
}

func TestSyncNoOpWhenUpToDate(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")
	m := newManager(t, remote, dir)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	res, err := m.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Fetched {
		t.Fatalf("expected Fetched=true")
	}
	if len(res.Changed) != 0 || len(res.Deleted) != 0 {
		t.Fatalf("expected empty diff, got changed=%v deleted=%v", res.Changed, res.Deleted)
	}
	if res.OldCommit != res.NewCommit {
		t.Fatalf("HEAD should not move on no-op sync: old=%s new=%s", res.OldCommit, res.NewCommit)
	}
	if m.Status().LastError != "" {
		t.Fatalf("unexpected LastError: %s", m.Status().LastError)
	}
	if m.Status().LastSyncAt == nil {
		t.Fatalf("expected LastSyncAt set after successful sync")
	}
}

func TestSyncDetectsUpstreamCommit(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")

	fixed := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	var notifications []string
	m, err := New(Config{
		Remote: remote, Ref: "main", Dir: dir,
		Now:    func() time.Time { return fixed },
		Notify: func(name string, _ any) { notifications = append(notifications, name) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Second clone pushes: add gamma.md, modify alpha.md, delete beta.md.
	work := filepath.Join(t.TempDir(), "work")
	git(t, "", "clone", "--branch", "main", remote, work)
	writeFile(t, filepath.Join(work, "gamma.md"), "# Gamma\n")
	writeFile(t, filepath.Join(work, "alpha.md"), "# Alpha edited\n")
	if err := os.Remove(filepath.Join(work, "beta.md")); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "-A")
	git(t, work, "commit", "-m", "update")
	git(t, work, "push", "origin", "main")

	res, err := m.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.OldCommit == res.NewCommit {
		t.Fatalf("expected HEAD to advance")
	}
	if !contains(res.Changed, "gamma.md") || !contains(res.Changed, "alpha.md") {
		t.Fatalf("Changed missing expected paths: %v", res.Changed)
	}
	if !contains(res.Deleted, "beta.md") {
		t.Fatalf("Deleted missing beta.md: %v", res.Deleted)
	}
	// Working copy reflects the changes.
	if _, err := os.Stat(filepath.Join(dir, "gamma.md")); err != nil {
		t.Fatalf("gamma.md not present after sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "beta.md")); !os.IsNotExist(err) {
		t.Fatalf("beta.md should be gone after sync, stat err=%v", err)
	}
	if !contains(notifications, "git:synced") {
		t.Fatalf("expected git:synced notification, got %v", notifications)
	}
	if got := m.Status().LastSyncAt; got == nil || !got.Equal(fixed) {
		t.Fatalf("LastSyncAt = %v, want %v", got, fixed)
	}
}

func TestSyncPreservesMetadataDir(t *testing.T) {
	requireGit(t)
	remote := makeRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")
	m := newManager(t, remote, dir) // MetadataDir defaults to ".workspace"
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Untracked index under the metadata dir.
	indexPath := filepath.Join(dir, ".workspace", "index.db")
	writeFile(t, indexPath, "derived index\n")

	// Also drop an untracked file OUTSIDE the metadata dir to prove clean runs.
	strayPath := filepath.Join(dir, "stray.tmp")
	writeFile(t, strayPath, "junk\n")

	// Advance the remote so HEAD moves (exercises the full path).
	work := filepath.Join(t.TempDir(), "work")
	git(t, "", "clone", "--branch", "main", remote, work)
	writeFile(t, filepath.Join(work, "delta.md"), "# Delta\n")
	git(t, work, "add", "-A")
	git(t, work, "commit", "-m", "delta")
	git(t, work, "push", "origin", "main")

	if _, err := m.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("metadata file should survive clean -e: %v", err)
	}
	if _, err := os.Stat(strayPath); !os.IsNotExist(err) {
		t.Fatalf("untracked stray file should have been cleaned, stat err=%v", err)
	}
}

// fakeRunner records nothing; it just replays canned outputs keyed by the
// first git subcommand, used to unit-test diff parsing without a real repo.
type fakeRunner struct {
	head    string
	newHead string
	diff    string
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	switch args[0] {
	case "rev-parse":
		if f.head == f.newHead {
			return f.head + "\n", nil
		}
		// First rev-parse returns old, subsequent returns new.
		out := f.head + "\n"
		f.head = f.newHead
		return out, nil
	case "fetch", "reset", "clean":
		return "", nil
	case "diff":
		return f.diff, nil
	}
	return "", nil
}

func TestParseNameStatusViaFakeRunner(t *testing.T) {
	// -z output: status\x00path\x00 ... rename is R100\x00old\x00new\x00
	diff := strings.Join([]string{
		"A", "gamma.md",
		"M", "alpha.md",
		"D", "beta.md",
		"R100", "old.md", "new.md",
	}, "\x00") + "\x00"

	fr := &fakeRunner{head: "1111111111112222222222", newHead: "3333333333334444444444", diff: diff}
	m, err := New(Config{Remote: "r", Dir: "d", Runner: fr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := m.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	changed := append([]string(nil), res.Changed...)
	sort.Strings(changed)
	wantChanged := []string{"alpha.md", "gamma.md", "new.md"}
	if !reflect.DeepEqual(changed, wantChanged) {
		t.Fatalf("Changed = %v, want %v", changed, wantChanged)
	}
	if !reflect.DeepEqual(res.Deleted, []string{"beta.md"}) {
		t.Fatalf("Deleted = %v, want [beta.md]", res.Deleted)
	}
	if len(res.Renamed) != 1 || res.Renamed[0] != [2]string{"old.md", "new.md"} {
		t.Fatalf("Renamed = %v, want [[old.md new.md]]", res.Renamed)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{Dir: "d"}); err == nil {
		t.Fatalf("expected error for empty Remote")
	}
	if _, err := New(Config{Remote: "r"}); err == nil {
		t.Fatalf("expected error for empty Dir")
	}
	m, err := New(Config{Remote: "r", Dir: "d"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.cfg.Ref != "main" || m.cfg.MetadataDir != ".workspace" {
		t.Fatalf("defaults not applied: ref=%q meta=%q", m.cfg.Ref, m.cfg.MetadataDir)
	}
	if m.cfg.Runner == nil || m.cfg.Now == nil {
		t.Fatalf("Runner/Now defaults not applied")
	}
}

func TestGitCredentialEnvUsesTemporaryExtraHeader(t *testing.T) {
	env := gitCredentialEnv(Credential{Username: "alice", Token: "secret-token"})
	wantHeader := "GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret-token"))
	if !contains(env, "GIT_CONFIG_COUNT=1") || !contains(env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") || !contains(env, wantHeader) {
		t.Fatalf("credential env missing expected git config entries: %v", env)
	}
	if !contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("credential env should disable interactive prompts: %v", env)
	}
}

func TestGitCredentialEnvDefaultsUsername(t *testing.T) {
	env := gitCredentialEnv(Credential{Token: "secret-token"})
	wantHeader := "GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:secret-token"))
	if !contains(env, wantHeader) {
		t.Fatalf("credential env missing default-token username header: %v", env)
	}
}

func TestRunPollZeroReturns(t *testing.T) {
	m, err := New(Config{Remote: "r", Dir: "d"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.RunPoll(context.Background(), 0); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("RunPoll(interval=0) should return immediately")
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
