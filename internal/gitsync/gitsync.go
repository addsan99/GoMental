// Package gitsync advances a git working copy that GoMental treats as a
// read-only source of truth for its notes. It is intentionally decoupled from
// the reconcile path: it only moves the working copy on disk (clone / fetch /
// reset / clean) and surfaces git-level state. The existing workspace watcher
// picks up the resulting file changes and does the real content reconcile.
package gitsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runner executes git subcommands in a working directory. execRunner (the
// default) shells out to the `git` binary; tests inject a fake.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (stdout string, err error)
}

// Config configures a Manager. Remote and Dir are required; everything else
// has a sensible default filled in by New.
type Config struct {
	Remote      string                         // e.g. https://github.com/org/wiki.git (required)
	Ref         string                         // branch/tag to track; default "main"
	Dir         string                         // working-copy path == workspace root (required)
	MetadataDir string                         // untracked dir to protect from clean; default ".workspace"
	Credential  Credential                     // optional app-managed HTTPS credential
	Runner      Runner                         // nil → execRunner
	Notify      func(name string, payload any) // nil → no-op; hub.Publish in prod
	Now         func() time.Time               // nil → time.Now (tests inject)
}

type Credential struct {
	Username string
	Token    string
}

func (c Credential) IsSet() bool {
	return strings.TrimSpace(c.Token) != ""
}

// Result reports what a single Sync did. Changed/Deleted/Renamed are
// informational only — the workspace watcher performs the real reconcile.
type Result struct {
	Fetched   bool
	OldCommit string
	NewCommit string
	Changed   []string    // repo-relative paths added/modified (informational)
	Deleted   []string    // repo-relative paths deleted (informational)
	Renamed   [][2]string // {old, new} repo-relative paths (informational)
}

// Status is a thread-safe snapshot of the manager's git-level state.
type Status struct {
	Remote     string
	Ref        string
	Commit     string // current HEAD short SHA ("" if not yet cloned)
	LastSyncAt *time.Time
	LastError  string
	Syncing    bool
	Operation  string
}

// Manager holds the frozen config plus a mutex-guarded last Status. All git
// mutations funnel through Sync, which is serialized.
type Manager struct {
	cfg Config

	mu     sync.Mutex // serializes Sync and guards status
	status Status
}

// execRunner is the default Runner. It shells to the `git` binary, captures
// stdout + stderr, and on failure returns an error carrying stderr.
type execRunner struct {
	credential Credential
}

func (r execRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	configureGitCommand(cmd)
	if dir != "" {
		cmd.Dir = dir
	}
	if r.credential.IsSet() {
		cmd.Env = append(os.Environ(), gitCredentialEnv(r.credential)...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return stdout.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
		}
		return stdout.String(), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// New validates the config, applies defaults, and returns a Manager. It does
// not touch the filesystem or the network — call Ensure for that.
func New(cfg Config) (*Manager, error) {
	if strings.TrimSpace(cfg.Remote) == "" {
		return nil, errors.New("gitsync: Remote is required")
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, errors.New("gitsync: Dir is required")
	}
	if cfg.Ref == "" {
		cfg.Ref = "main"
	}
	if cfg.MetadataDir == "" {
		cfg.MetadataDir = ".workspace"
	}
	if cfg.Runner == nil {
		cfg.Runner = execRunner{credential: cfg.Credential}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	m := &Manager{cfg: cfg}
	m.status = Status{Remote: cfg.Remote, Ref: cfg.Ref}
	return m, nil
}

func gitCredentialEnv(credential Credential) []string {
	username := strings.TrimSpace(credential.Username)
	if username == "" {
		username = "x-access-token"
	}
	value := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+credential.Token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=" + value,
		"GIT_TERMINAL_PROMPT=0",
	}
}

// notify safely invokes the configured Notify callback (no-op if nil).
func (m *Manager) notify(name string, payload any) {
	if m.cfg.Notify != nil {
		m.cfg.Notify(name, payload)
	}
}

// Ensure makes sure Dir is a git working copy tracking the configured remote.
//   - Dir absent or empty → git clone --branch <ref> <remote> <dir>.
//   - Dir contains a .git → verify it's a work tree and leave it.
//   - Dir non-empty but not a repo → error.
func (m *Manager) Ensure(ctx context.Context) error {
	dir := m.cfg.Dir

	// If a .git exists, verify it is a working tree and adopt it.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if _, err := m.cfg.Runner.Run(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
			return fmt.Errorf("gitsync: %s has a .git but is not a valid work tree: %w", dir, err)
		}
		m.refreshCommit(ctx)
		return nil
	}

	empty, err := dirEmptyOrAbsent(dir)
	if err != nil {
		return fmt.Errorf("gitsync: inspecting %s: %w", dir, err)
	}
	if !empty {
		return fmt.Errorf("gitsync: %s is not empty and is not a git repository; refusing to clone into it", dir)
	}

	// Create the parent so `git clone` can create dir itself.
	if parent := filepath.Dir(dir); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("gitsync: creating parent of %s: %w", dir, err)
		}
	}
	// If dir exists but is empty, git clone into it is fine; if absent, clone creates it.
	if _, err := m.cfg.Runner.Run(ctx, "", "clone", "--branch", m.cfg.Ref, m.cfg.Remote, dir); err != nil {
		return fmt.Errorf("gitsync: clone failed: %w", err)
	}
	m.refreshCommit(ctx)
	return nil
}

// refreshCommit reads HEAD and stores its short SHA in the status snapshot.
func (m *Manager) refreshCommit(ctx context.Context) {
	out, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-parse", "HEAD")
	if err != nil {
		return
	}
	head := strings.TrimSpace(out)
	m.mu.Lock()
	m.status.Commit = short(head)
	m.mu.Unlock()
}

// Sync advances the working copy to origin/<ref> and reports what changed.
// It is mutex-serialized: a second concurrent call blocks until the first
// finishes, then runs its own sync (git is never invoked concurrently). Steps:
// record old HEAD, fetch, reset --hard, clean -fd -e <metadata> (never -x),
// record new HEAD, and if it moved, diff old..new for informational paths.
func (m *Manager) Sync(ctx context.Context) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.Syncing = true
	m.status.Operation = "Syncing local notes from git"
	m.notifyStatusLocked()
	defer func() {
		m.status.Syncing = false
		m.status.Operation = ""
		m.notifyStatusLocked()
	}()

	res := Result{}

	fail := func(err error) (Result, error) {
		m.status.LastError = err.Error()
		m.notify("git:sync-error", map[string]any{"error": err.Error()})
		return res, err
	}

	// 1. Record old HEAD.
	oldOut, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-parse", "HEAD")
	if err != nil {
		return fail(fmt.Errorf("gitsync: reading current HEAD: %w", err))
	}
	oldHead := strings.TrimSpace(oldOut)
	res.OldCommit = oldHead

	// 2. Fetch the tracked ref.
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "fetch", "origin", m.cfg.Ref); err != nil {
		return fail(fmt.Errorf("gitsync: fetch failed: %w", err))
	}
	res.Fetched = true

	// 3. Hard reset onto the fetched ref.
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "reset", "--hard", "origin/"+m.cfg.Ref); err != nil {
		return fail(fmt.Errorf("gitsync: reset failed: %w", err))
	}

	// 4. Clean untracked files, but NEVER -x, and always preserve the metadata dir.
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "clean", "-fd", "-e", m.cfg.MetadataDir); err != nil {
		return fail(fmt.Errorf("gitsync: clean failed: %w", err))
	}

	// 5. Record new HEAD.
	newOut, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-parse", "HEAD")
	if err != nil {
		return fail(fmt.Errorf("gitsync: reading new HEAD: %w", err))
	}
	newHead := strings.TrimSpace(newOut)
	res.NewCommit = newHead

	// 6. If HEAD moved, gather informational diff paths.
	if newHead != oldHead {
		diffOut, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "diff", "--name-status", "-z", oldHead, newHead)
		if err != nil {
			return fail(fmt.Errorf("gitsync: diff failed: %w", err))
		}
		changed, deleted, renamed := parseNameStatus(diffOut)
		res.Changed = changed
		res.Deleted = deleted
		res.Renamed = renamed
	}

	// Success: update status.
	now := m.cfg.Now()
	m.status.Commit = short(newHead)
	m.status.LastSyncAt = &now
	m.status.LastError = ""

	m.notify("git:synced", map[string]any{
		"commit":    short(newHead),
		"oldCommit": short(oldHead),
		"changed":   len(res.Changed),
		"deleted":   len(res.Deleted),
	})
	return res, nil
}

// Status returns a thread-safe snapshot of the current git-level state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status // copy
	if m.status.LastSyncAt != nil {
		t := *m.status.LastSyncAt
		s.LastSyncAt = &t
	}
	return s
}

func (m *Manager) notifyStatusLocked() {
	m.notify("git:status", map[string]any{
		"remote":    m.status.Remote,
		"ref":       m.status.Ref,
		"commit":    m.status.Commit,
		"lastError": m.status.LastError,
		"syncing":   m.status.Syncing,
		"operation": m.status.Operation,
	})
}

// RunPoll drives Sync on a ticker until ctx is done. interval<=0 returns
// immediately (polling disabled — the deployment is webhook-driven). Per-sync
// errors are swallowed here because they are already recorded in Status and
// surfaced via Notify.
func (m *Manager) RunPoll(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.Sync(ctx)
		}
	}
}

// short returns the first 12 chars of a SHA (guarding short strings).
func short(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

// dirEmptyOrAbsent reports whether dir does not exist or exists with no entries.
func dirEmptyOrAbsent(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

// parseNameStatus parses the output of `git diff --name-status -z <old> <new>`.
// The -z form is NUL-separated. Each record is a status field followed by one
// path (A/M/D) or two paths (R<score>/C<score>). Returns changed (A, M, C
// destination), deleted (D), and renamed ({old,new}) paths.
func parseNameStatus(out string) (changed, deleted []string, renamed [][2]string) {
	fields := strings.Split(out, "\x00")
	// Drop a trailing empty field produced by the final NUL.
	i := 0
	next := func() (string, bool) {
		for i < len(fields) {
			f := fields[i]
			i++
			if f == "" {
				continue
			}
			return f, true
		}
		return "", false
	}
	for {
		status, ok := next()
		if !ok {
			break
		}
		code := status[0]
		switch code {
		case 'R', 'C':
			// rename/copy: status is like "R100"; two paths follow.
			from, ok1 := next()
			to, ok2 := next()
			if !ok1 || !ok2 {
				return
			}
			if code == 'R' {
				renamed = append(renamed, [2]string{from, to})
				// The destination is new content; surface it as changed too.
				changed = append(changed, to)
			} else { // copy
				changed = append(changed, to)
			}
		case 'D':
			path, ok := next()
			if !ok {
				return
			}
			deleted = append(deleted, path)
		case 'A', 'M', 'T':
			path, ok := next()
			if !ok {
				return
			}
			changed = append(changed, path)
		default:
			// Unknown/extended status (e.g. U for unmerged): consume one path.
			if _, ok := next(); !ok {
				return
			}
		}
	}
	return changed, deleted, renamed
}
