package gitsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type WritableConfig struct {
	Remote      string
	BaseRef     string
	Branch      string
	Dir         string
	ContentPath string
	MetadataDir string
	Credential  Credential
	Runner      Runner
	Notify      func(name string, payload any)
	Now         func() time.Time
	HTTPClient  *http.Client
}

const DefaultWritableContentPath = ".GoMental"

type WritableStatus struct {
	Remote       string     `json:"remote"`
	BaseRef      string     `json:"baseRef"`
	Branch       string     `json:"branch"`
	Commit       string     `json:"commit"`
	Ahead        int        `json:"ahead"`
	Dirty        bool       `json:"dirty"`
	Syncing      bool       `json:"syncing"`
	PullRequest  string     `json:"pullRequest"`
	LastPushedAt *time.Time `json:"lastPushedAt"`
	LastError    string     `json:"lastError"`
	Operation    string     `json:"operation"`
}

type CommitResult struct {
	Committed bool
	Pushed    bool
	Commit    string
}

type PullRequestResult struct {
	URL    string
	Number int
	Merged bool
}

type WritableManager struct {
	cfg WritableConfig

	mu     sync.Mutex
	status WritableStatus
}

func NewWritable(cfg WritableConfig) (*WritableManager, error) {
	if strings.TrimSpace(cfg.Remote) == "" {
		return nil, errors.New("gitsync writable: Remote is required")
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, errors.New("gitsync writable: Dir is required")
	}
	if cfg.BaseRef == "" {
		cfg.BaseRef = "main"
	}
	if cfg.Branch == "" {
		cfg.Branch = DefaultInstanceBranch(cfg.Dir)
	}
	if cfg.MetadataDir == "" {
		cfg.MetadataDir = ".workspace"
	}
	contentPath, err := normalizeContentPath(cfg.ContentPath)
	if err != nil {
		return nil, err
	}
	cfg.ContentPath = contentPath
	if cfg.Runner == nil {
		cfg.Runner = execRunner{credential: cfg.Credential}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	m := &WritableManager{cfg: cfg}
	m.status = WritableStatus{Remote: cfg.Remote, BaseRef: cfg.BaseRef, Branch: cfg.Branch}
	return m, nil
}

// WorkspaceDir is the note workspace inside the Git checkout.
func (m *WritableManager) WorkspaceDir() string {
	dir, _ := ResolveWritableContentDir(m.cfg.Dir, m.cfg.ContentPath)
	return dir
}

// ResolveWritableContentDir returns the note root for a repository checkout.
func ResolveWritableContentDir(repoRoot, contentPath string) (string, error) {
	normalized, err := normalizeContentPath(contentPath)
	if err != nil {
		return "", err
	}
	root := filepath.Clean(repoRoot)
	if normalized == "." {
		return root, nil
	}
	return filepath.Join(root, filepath.FromSlash(normalized)), nil
}

func DefaultInstanceBranch(workspaceDir string) string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "machine"
	}
	slug := filepath.Base(filepath.Clean(workspaceDir))
	return "gomental/" + sanitizeBranchPart(host) + "/" + sanitizeBranchPart(slug)
}

func (m *WritableManager) Ensure(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beginLocked("Syncing local notes from git")
	defer m.finishLocked()

	dir := m.cfg.Dir
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		empty, eerr := dirEmptyOrAbsent(dir)
		if eerr != nil {
			return m.fail(fmt.Errorf("gitsync writable: inspecting %s: %w", dir, eerr))
		}
		if !empty {
			return m.fail(fmt.Errorf("gitsync writable: %s is not empty and is not a git repository", dir))
		}
		if parent := filepath.Dir(dir); parent != "" {
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return m.fail(fmt.Errorf("gitsync writable: creating parent of %s: %w", dir, err))
			}
		}
		if _, err := m.cfg.Runner.Run(ctx, "", "clone", m.cfg.Remote, dir); err != nil {
			return m.fail(fmt.Errorf("gitsync writable: clone failed: %w", err))
		}
	} else if _, err := m.cfg.Runner.Run(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return m.fail(fmt.Errorf("gitsync writable: %s has a .git but is not a valid work tree: %w", dir, err))
	}

	if _, err := m.cfg.Runner.Run(ctx, dir, "config", "user.name", "GoMental"); err != nil {
		return m.fail(fmt.Errorf("gitsync writable: configure git user.name: %w", err))
	}
	if _, err := m.cfg.Runner.Run(ctx, dir, "config", "user.email", "gomental@local"); err != nil {
		return m.fail(fmt.Errorf("gitsync writable: configure git user.email: %w", err))
	}
	if err := m.ensureBaseFetchedLocked(ctx); err != nil {
		return m.fail(err)
	}
	_, _ = m.cfg.Runner.Run(ctx, dir, "fetch", "origin", m.cfg.Branch+":"+m.cfg.Branch)

	if _, err := m.cfg.Runner.Run(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+m.cfg.Branch); err == nil {
		if _, err := m.cfg.Runner.Run(ctx, dir, "checkout", m.cfg.Branch); err != nil {
			return m.fail(fmt.Errorf("gitsync writable: checkout branch failed: %w", err))
		}
	} else if _, err := m.cfg.Runner.Run(ctx, dir, "checkout", "-B", m.cfg.Branch, "origin/"+m.cfg.BaseRef); err != nil {
		return m.fail(fmt.Errorf("gitsync writable: create branch failed: %w", err))
	}
	if _, err := m.cfg.Runner.Run(ctx, dir, "merge", "--no-edit", "origin/"+m.cfg.BaseRef); err != nil {
		return m.fail(fmt.Errorf("gitsync writable: merge base failed: %w", err))
	}
	if err := os.MkdirAll(m.WorkspaceDir(), 0o755); err != nil {
		return m.fail(fmt.Errorf("gitsync writable: creating content directory: %w", err))
	}
	m.refreshLocked(ctx)
	return nil
}

func (m *WritableManager) CommitAndPush(ctx context.Context, message string, paths []string) (CommitResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beginLocked("Committing local notes to git")
	defer m.finishLocked()

	paths = cleanRelPaths(paths, m.cfg.MetadataDir)
	if len(paths) == 0 {
		return CommitResult{}, nil
	}
	paths = m.repoPaths(paths)
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, args...); err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: stage changes: %w", err))
	}
	statusArgs := append([]string{"status", "--porcelain", "--"}, paths...)
	out, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, statusArgs...)
	if err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: inspect changes: %w", err))
	}
	if strings.TrimSpace(out) == "" {
		m.refreshLocked(ctx)
		return CommitResult{Commit: m.status.Commit}, nil
	}
	if strings.TrimSpace(message) == "" {
		message = "Update notes from GoMental"
	}
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "commit", "-m", message); err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: commit changes: %w", err))
	}
	head, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-parse", "HEAD")
	if err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: read commit: %w", err))
	}
	m.setOperationLocked("Pushing local notes to git")
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "push", "-u", "origin", m.cfg.Branch); err != nil {
		m.refreshLocked(ctx)
		return CommitResult{Committed: true, Commit: short(strings.TrimSpace(head))}, m.fail(fmt.Errorf("gitsync writable: push branch: %w", err))
	}
	now := m.cfg.Now()
	m.status.LastPushedAt = &now
	m.status.LastError = ""
	m.refreshLocked(ctx)
	m.notify("git:pushed", map[string]any{"branch": m.cfg.Branch, "commit": m.status.Commit})
	return CommitResult{Committed: true, Pushed: true, Commit: m.status.Commit}, nil
}

func (m *WritableManager) CommitAll(ctx context.Context, message string) (CommitResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beginLocked("Committing local notes to git")
	defer m.finishLocked()

	root, exclusions := m.workspacePathspecs()
	args := append([]string{"add", "-A", "--", root}, exclusions...)
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, args...); err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: stage workspace changes: %w", err))
	}
	statusArgs := append([]string{"status", "--porcelain", "--", root}, exclusions...)
	out, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, statusArgs...)
	if err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: inspect workspace changes: %w", err))
	}
	if strings.TrimSpace(out) == "" {
		m.refreshLocked(ctx)
		return CommitResult{Commit: m.status.Commit}, nil
	}
	if strings.TrimSpace(message) == "" {
		message = "Update workspace from GoMental"
	}
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "commit", "-m", message); err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: commit workspace changes: %w", err))
	}
	head, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-parse", "HEAD")
	if err != nil {
		return CommitResult{}, m.fail(fmt.Errorf("gitsync writable: read commit: %w", err))
	}
	m.refreshLocked(ctx)
	return CommitResult{Committed: true, Commit: short(strings.TrimSpace(head))}, nil
}

func (m *WritableManager) OpenPullRequest(ctx context.Context, title, body string) (PullRequestResult, error) {
	return m.createOrMergePullRequest(ctx, title, body, false)
}

func (m *WritableManager) MergePullRequest(ctx context.Context, title, body string) (PullRequestResult, error) {
	return m.createOrMergePullRequest(ctx, title, body, true)
}

func (m *WritableManager) createOrMergePullRequest(ctx context.Context, title, body string, merge bool) (PullRequestResult, error) {
	if !m.cfg.Credential.IsSet() {
		return PullRequestResult{}, m.fail(errors.New("gitsync writable: GitHub token is required to open or merge a pull request"))
	}
	repo, err := parseGitHubRepo(m.cfg.Remote)
	if err != nil {
		return PullRequestResult{}, m.fail(err)
	}
	if strings.TrimSpace(title) == "" {
		title = "Update GoMental workspace"
	}
	defer m.finish()
	if _, err := m.CommitAll(ctx, "Update workspace from GoMental"); err != nil {
		return PullRequestResult{}, m.fail(err)
	}
	if err := m.pushBranch(ctx); err != nil {
		return PullRequestResult{}, m.fail(fmt.Errorf("gitsync writable: push branch before pull request: %w", err))
	}
	m.setOperation("Opening pull request")
	pr, err := m.findOpenPullRequest(ctx, repo)
	if err != nil {
		return PullRequestResult{}, m.fail(err)
	}
	if pr.Number == 0 {
		pr, err = m.createPullRequest(ctx, repo, title, body)
		if err != nil {
			return PullRequestResult{}, m.fail(err)
		}
	}
	m.mu.Lock()
	m.status.PullRequest = pr.URL
	m.status.LastError = ""
	m.mu.Unlock()
	m.notify("git:pr", map[string]any{"url": pr.URL, "number": pr.Number})
	if !merge {
		return pr, nil
	}
	m.setOperation("Merging pull request")
	if err := m.mergePullRequest(ctx, repo, pr.Number); err != nil {
		return PullRequestResult{}, m.fail(err)
	}
	pr.Merged = true
	m.notify("git:merged", map[string]any{"url": pr.URL, "number": pr.Number})
	return pr, nil
}

func (m *WritableManager) Status() WritableStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	if m.status.LastPushedAt != nil {
		t := *m.status.LastPushedAt
		s.LastPushedAt = &t
	}
	return s
}

func (m *WritableManager) pushBranch(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beginLocked("Pushing local notes to git")
	defer m.finishLocked()

	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "push", "-u", "origin", m.cfg.Branch); err != nil {
		m.refreshLocked(ctx)
		return err
	}
	now := m.cfg.Now()
	m.status.LastPushedAt = &now
	m.status.LastError = ""
	m.refreshLocked(ctx)
	m.notify("git:pushed", map[string]any{"branch": m.cfg.Branch, "commit": m.status.Commit})
	return nil
}

func (m *WritableManager) refreshLocked(ctx context.Context) {
	if out, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-parse", "HEAD"); err == nil {
		m.status.Commit = short(strings.TrimSpace(out))
	}
	root, exclusions := m.workspacePathspecs()
	statusArgs := append([]string{"status", "--porcelain", "--", root}, exclusions...)
	if out, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, statusArgs...); err == nil {
		m.status.Dirty = strings.TrimSpace(out) != ""
	}
	if out, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "rev-list", "--count", "origin/"+m.cfg.BaseRef+"..HEAD"); err == nil {
		var ahead int
		_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &ahead)
		m.status.Ahead = ahead
	}
}

func (m *WritableManager) ensureBaseFetchedLocked(ctx context.Context) error {
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "fetch", "origin", m.cfg.BaseRef); err == nil {
		return nil
	} else if m.cfg.BaseRef != "main" {
		return fmt.Errorf("gitsync writable: fetch base failed: %w", err)
	}
	if _, err := m.cfg.Runner.Run(ctx, m.cfg.Dir, "fetch", "origin", "master"); err != nil {
		return fmt.Errorf("gitsync writable: fetch base failed: %w", err)
	}
	m.cfg.BaseRef = "master"
	m.status.BaseRef = "master"
	return nil
}

func (m *WritableManager) fail(err error) error {
	m.status.LastError = err.Error()
	m.notify("git:error", map[string]any{"error": err.Error()})
	return err
}

func (m *WritableManager) notify(name string, payload any) {
	if m.cfg.Notify != nil {
		m.cfg.Notify(name, payload)
	}
}

func (m *WritableManager) beginLocked(operation string) {
	m.status.Syncing = true
	m.status.Operation = operation
	m.notifyStatusLocked()
}

func (m *WritableManager) setOperation(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setOperationLocked(operation)
}

func (m *WritableManager) setOperationLocked(operation string) {
	m.status.Syncing = true
	m.status.Operation = operation
	m.notifyStatusLocked()
}

func (m *WritableManager) finishLocked() {
	m.status.Syncing = false
	m.status.Operation = ""
	m.notifyStatusLocked()
}

func (m *WritableManager) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishLocked()
}

func (m *WritableManager) notifyStatusLocked() {
	m.notify("git:status", map[string]any{
		"remote":       m.status.Remote,
		"ref":          m.status.Branch,
		"baseRef":      m.status.BaseRef,
		"branch":       m.status.Branch,
		"commit":       m.status.Commit,
		"ahead":        m.status.Ahead,
		"dirty":        m.status.Dirty,
		"pullRequest":  m.status.PullRequest,
		"lastError":    m.status.LastError,
		"syncing":      m.status.Syncing,
		"operation":    m.status.Operation,
		"lastPushedAt": m.status.LastPushedAt,
	})
}

type githubRepo struct {
	Owner string
	Name  string
}

type githubPR struct {
	URL    string
	Number int
	Merged bool
}

func parseGitHubRepo(remote string) (githubRepo, error) {
	remote = strings.TrimSpace(remote)
	if u, err := url.Parse(remote); err == nil && strings.EqualFold(u.Host, "github.com") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			return githubRepo{Owner: parts[0], Name: strings.TrimSuffix(parts[1], ".git")}, nil
		}
	}
	re := regexp.MustCompile(`^git@github\.com:([^/]+)/(.+?)(?:\.git)?$`)
	if m := re.FindStringSubmatch(remote); len(m) == 3 {
		return githubRepo{Owner: m[1], Name: strings.TrimSuffix(m[2], ".git")}, nil
	}
	return githubRepo{}, fmt.Errorf("gitsync writable: %q is not a supported GitHub remote URL", remote)
}

func (m *WritableManager) findOpenPullRequest(ctx context.Context, repo githubRepo) (PullRequestResult, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls?state=open&head=%s:%s&base=%s", url.PathEscape(repo.Owner), url.PathEscape(repo.Name), url.QueryEscape(repo.Owner), url.QueryEscape(m.cfg.Branch), url.QueryEscape(m.cfg.BaseRef))
	var prs []struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := m.githubJSON(ctx, http.MethodGet, path, nil, &prs); err != nil {
		return PullRequestResult{}, err
	}
	if len(prs) == 0 {
		return PullRequestResult{}, nil
	}
	return PullRequestResult{URL: prs[0].HTMLURL, Number: prs[0].Number}, nil
}

func (m *WritableManager) createPullRequest(ctx context.Context, repo githubRepo, title, body string) (PullRequestResult, error) {
	payload := map[string]string{
		"title": title,
		"body":  body,
		"head":  repo.Owner + ":" + m.cfg.Branch,
		"base":  m.cfg.BaseRef,
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	path := fmt.Sprintf("repos/%s/%s/pulls", url.PathEscape(repo.Owner), url.PathEscape(repo.Name))
	if err := m.githubJSON(ctx, http.MethodPost, path, payload, &pr); err != nil {
		return PullRequestResult{}, err
	}
	return PullRequestResult{URL: pr.HTMLURL, Number: pr.Number}, nil
}

func (m *WritableManager) mergePullRequest(ctx context.Context, repo githubRepo, number int) error {
	payload := map[string]string{"merge_method": "merge"}
	path := fmt.Sprintf("repos/%s/%s/pulls/%d/merge", url.PathEscape(repo.Owner), url.PathEscape(repo.Name), number)
	return m.githubJSON(ctx, http.MethodPut, path, payload, nil)
}

func (m *WritableManager) githubJSON(ctx context.Context, method, apiPath string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com/"+apiPath, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+m.cfg.Credential.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api %s %s: %s: %s", method, apiPath, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func sanitizeBranchPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "workspace"
	}
	return out
}

func cleanRelPaths(paths []string, metadataDir string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	metadataDir = filepath.ToSlash(filepath.Clean(strings.TrimSpace(metadataDir)))
	for _, p := range paths {
		p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
		if p == "" || p == "." || filepath.IsAbs(p) || strings.HasPrefix(p, "../") || strings.HasPrefix(p, metadataDir+"/") || p == metadataDir {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func normalizeContentPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = DefaultWritableContentPath
	}
	value = strings.ReplaceAll(value, `\`, "/")
	if filepath.IsAbs(filepath.FromSlash(value)) || filepath.VolumeName(filepath.FromSlash(value)) != "" {
		return "", errors.New("gitsync writable: content path must be relative to the repository")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("gitsync writable: content path escapes the repository")
	}
	first := strings.Split(clean, "/")[0]
	if strings.EqualFold(first, ".git") {
		return "", errors.New("gitsync writable: content path cannot be inside .git")
	}
	return clean, nil
}

func (m *WritableManager) repoPaths(paths []string) []string {
	if m.cfg.ContentPath == "." {
		return paths
	}
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = m.cfg.ContentPath + "/" + path
	}
	return out
}

func (m *WritableManager) workspacePathspecs() (string, []string) {
	root := m.cfg.ContentPath
	metadata := m.cfg.MetadataDir
	if root != "." {
		metadata = root + "/" + metadata
	}
	return root, metadataExcludePathspecs(metadata)
}

func metadataExcludePathspecs(metadataDir string) []string {
	metadataDir = filepath.ToSlash(filepath.Clean(strings.TrimSpace(metadataDir)))
	if metadataDir == "" || metadataDir == "." {
		metadataDir = ".workspace"
	}
	return []string{":(exclude)" + metadataDir, ":(exclude)" + metadataDir + "/**"}
}
