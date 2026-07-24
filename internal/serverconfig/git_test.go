package serverconfig

import (
	"path/filepath"
	"testing"
	"time"
)

// In git-viewer mode the working copy may not exist yet (it is cloned on
// startup), so a missing workspace dir must be accepted, and read-only defaults on.
func TestGitModeAllowsMissingWorkspaceAndDefaultsReadOnly(t *testing.T) {
	target := filepath.Join(t.TempDir(), "clone-here") // does not exist yet
	cfg, err := Load(Options{WorkspaceRoot: target, GitRemote: "https://example.com/org/wiki.git"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.GitEnabled() {
		t.Fatal("expected git mode enabled")
	}
	if cfg.GitRef != DefaultGitRef {
		t.Fatalf("expected default ref %q, got %q", DefaultGitRef, cfg.GitRef)
	}
	if !cfg.ReadOnly {
		t.Fatal("read-only should default ON when a git remote is set")
	}
	if cfg.GitPollInterval != 0 {
		t.Fatalf("expected poll disabled by default, got %v", cfg.GitPollInterval)
	}
}

func TestReadOnlyExplicitOverrideBeatsGitDefault(t *testing.T) {
	target := filepath.Join(t.TempDir(), "clone-here")
	cfg, err := Load(Options{WorkspaceRoot: target, GitRemote: "https://example.com/w.git", ReadOnly: "false"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReadOnly {
		t.Fatal("explicit --read-only=false must override the git default")
	}
}

func TestReadOnlyDefaultsOffWithoutGit(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReadOnly {
		t.Fatal("read-only should be off without a git remote")
	}
}

func TestReadOnlyEnabledWithoutGit(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(Options{WorkspaceRoot: root, ReadOnly: "true"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.ReadOnly {
		t.Fatal("explicit --read-only=true should be honored without git")
	}
}

func TestGitPollDurationParsing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "clone-here")
	cfg, err := Load(Options{WorkspaceRoot: target, GitRemote: "https://x/w.git", GitPoll: "2m"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GitPollInterval != 2*time.Minute {
		t.Fatalf("expected 2m, got %v", cfg.GitPollInterval)
	}

	if _, err := Load(Options{WorkspaceRoot: target, GitRemote: "https://x/w.git", GitPoll: "nonsense"}); err == nil {
		t.Fatal("expected error for invalid poll duration")
	}
}

// Without a git remote, a missing workspace directory must still be rejected.
func TestMissingWorkspaceRejectedWithoutGit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := Load(Options{WorkspaceRoot: target}); err == nil {
		t.Fatal("expected error for missing workspace when git is disabled")
	}
}
