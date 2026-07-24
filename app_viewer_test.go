package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/application"
	"GoMental/internal/gitsync"
	"GoMental/internal/serverconfig"
)

func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// In read-only viewer mode the content-mutating bindings must reject before
// touching the service, mirroring the server's workspace.read_only 403.
func TestViewerReadOnlyBlocksWrites(t *testing.T) {
	app := NewViewerApp(serverconfig.Config{
		WorkspaceRoot: t.TempDir(),
		GitRemote:     "https://example.com/org/wiki.git",
		GitRef:        "main",
		ReadOnly:      true,
	}, testLogger())

	if _, err := app.SaveNote(application.SaveNoteRequest{}); !errors.Is(err, errReadOnly) {
		t.Fatalf("SaveNote: want errReadOnly, got %v", err)
	}
	if err := app.DeleteNote("x"); !errors.Is(err, errReadOnly) {
		t.Fatalf("DeleteNote: want errReadOnly, got %v", err)
	}
	if _, err := app.ImportURL(application.ImportURLRequest{}); !errors.Is(err, errReadOnly) {
		t.Fatalf("ImportURL: want errReadOnly, got %v", err)
	}
	if _, err := app.SaveNoteAsset(application.SaveNoteAssetRequest{}); !errors.Is(err, errReadOnly) {
		t.Fatalf("SaveNoteAsset: want errReadOnly, got %v", err)
	}
}

// A viewer that is NOT read-only (e.g. `viewer --workspace X` without git) must
// not short-circuit writes with errReadOnly.
func TestViewerWritableDoesNotBlock(t *testing.T) {
	app := NewViewerApp(serverconfig.Config{WorkspaceRoot: t.TempDir(), ReadOnly: false}, testLogger())
	if app.writesBlocked() {
		t.Fatal("writesBlocked should be false when ReadOnly is off")
	}
}

// Info() advertises viewer mode, the pinned workspace, read-only, and — before
// the manager exists — the git target so the chip renders immediately.
func TestViewerInfoShape(t *testing.T) {
	root := t.TempDir()
	app := NewViewerApp(serverconfig.Config{
		WorkspaceRoot: root,
		GitRemote:     "https://example.com/org/wiki.git",
		GitRef:        "dev",
		ReadOnly:      true,
	}, testLogger())

	info := app.Info()
	if info.Mode != "viewer" {
		t.Fatalf("mode: want viewer, got %q", info.Mode)
	}
	if info.Workspace != root {
		t.Fatalf("workspace: want %q, got %q", root, info.Workspace)
	}
	if !info.ReadOnly {
		t.Fatal("readOnly: want true")
	}
	if info.Git == nil || info.Git.Remote != "https://example.com/org/wiki.git" || info.Git.Ref != "dev" {
		t.Fatalf("git: want remote/ref advertised, got %#v", info.Git)
	}
	if info.Git.Commit != "" {
		t.Fatalf("git commit should be empty before first clone, got %q", info.Git.Commit)
	}
}

// Plain (non-git) desktop mode leaves the viewer fields empty so the SPA behaves
// exactly as the default build.
func TestDefaultAppInfoOmitsViewerFields(t *testing.T) {
	info := NewApp().Info()
	if info.Mode != "" || info.Workspace != "" || info.ReadOnly || info.Git != nil {
		t.Fatalf("default Info() must omit viewer fields, got %#v", info)
	}
}

// GitSync is only meaningful in git mode; without a manager it errors rather
// than panicking.
func TestViewerGitSyncWithoutManager(t *testing.T) {
	app := NewViewerApp(serverconfig.Config{WorkspaceRoot: t.TempDir()}, testLogger())
	if _, err := app.GitSync(); err == nil {
		t.Fatal("GitSync without a manager should error")
	}
}

func TestGitStatusToJSON(t *testing.T) {
	ts := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	got := gitStatusToJSON(gitsync.Status{
		Remote: "r", Ref: "main", Commit: "abcdef012345", LastSyncAt: &ts, LastError: "boom", Syncing: true,
	})
	if got.LastSyncAt == nil || *got.LastSyncAt != "2026-07-20T09:30:00Z" {
		t.Fatalf("lastSyncAt: got %v", got.LastSyncAt)
	}
	if got.Commit != "abcdef012345" || got.LastError != "boom" || !got.Syncing {
		t.Fatalf("unexpected status json: %#v", got)
	}

	nilTime := gitStatusToJSON(gitsync.Status{Remote: "r", Ref: "main"})
	if nilTime.LastSyncAt != nil {
		t.Fatalf("lastSyncAt should be nil when unset, got %v", *nilTime.LastSyncAt)
	}
}

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// makeBareRemote builds a bare repo on `main` holding a single note, and returns
// its path for cloning.
func makeBareRemote(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	work := t.TempDir()
	git(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "alpha.md"),
		[]byte("---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "init")

	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, t.TempDir(), "clone", "--bare", work, bare)
	return bare
}

// End-to-end: the desktop viewer clones the remote on first OpenWorkspace (before
// any .workspace metadata is written), serves its notes, and rejects writes.
func TestViewerEndToEndClone(t *testing.T) {
	bare := makeBareRemote(t)
	cloneTarget := filepath.Join(t.TempDir(), "working-copy") // does not exist yet

	cfg, err := serverconfig.Load(serverconfig.Options{WorkspaceRoot: cloneTarget, GitRemote: bare, GitRef: "main"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.ReadOnly {
		t.Fatal("read-only should default on in git mode")
	}

	app := NewViewerApp(cfg, testLogger())
	t.Cleanup(func() {
		app.mu.Lock()
		host := app.host
		app.mu.Unlock()
		if host != nil {
			_ = host.Close()
		}
	})

	// Replicate startup()'s manager build (without launching Wails): a Desktop
	// host works headless, differing only by NativeDialogs.
	app.ctx = context.Background()
	host := app.mustHost()
	mgr, err := gitsync.New(gitsync.Config{Remote: cfg.GitRemote, Ref: cfg.GitRef, Dir: cfg.WorkspaceRoot, Notify: host.Hub().Publish})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	app.viewer.mgr = mgr

	// First OpenWorkspace clones then opens; the arg is ignored in favor of the
	// pinned root.
	if _, err := app.OpenWorkspace("ignored"); err != nil {
		t.Fatalf("open workspace (clone): %v", err)
	}

	notes, err := app.ListNotes()
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("want 1 cloned note, got %d", len(notes))
	}

	// Writes are rejected in read-only viewer mode.
	if err := app.DeleteNote(notes[0].ID); !errors.Is(err, errReadOnly) {
		t.Fatalf("DeleteNote in read-only viewer: want errReadOnly, got %v", err)
	}

	// Info() now reports the real cloned commit.
	info := app.Info()
	if info.Git == nil || info.Git.Commit == "" {
		t.Fatalf("Info().git should report a commit after clone, got %#v", info.Git)
	}
}
