package mobile

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"GoMental/internal/domain"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

type syncRequest struct {
	Remote   string `json:"remote"`
	Ref      string `json:"ref"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

type syncResult struct {
	Cloned    bool   `json:"cloned"`
	Fetched   bool   `json:"fetched"`
	Changed   bool   `json:"changed"`
	OldCommit string `json:"oldCommit,omitempty"`
	NewCommit string `json:"newCommit"`
	NoteCount int    `json:"noteCount"`
}

// Sync clones or fetches the configured HTTPS repository, activates the
// requested branch, and rebuilds the notes projection. Credentials are used
// only for this call and are never written to the repository configuration.
func (c *Core) Sync(requestJSON string) (string, error) {
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()

	var request syncRequest
	if err := decodeJSON(requestJSON, &request); err != nil {
		return "", fmt.Errorf("mobile: invalid sync request: %w", err)
	}
	request.Remote = strings.TrimSpace(request.Remote)
	request.Ref = strings.TrimSpace(request.Ref)
	if request.Remote == "" {
		return "", errors.New("mobile: sync remote is required")
	}
	if request.Ref == "" {
		request.Ref = "main"
	}
	if err := plumbing.NewBranchReferenceName(request.Ref).Validate(); err != nil {
		return "", fmt.Errorf("mobile: invalid sync ref: %w", err)
	}
	if err := validateRemote(request.Remote); err != nil {
		return "", err
	}
	auth := gitAuth(request.Username, request.Token)

	repository, cloned, err := c.openOrClone(ctx, request, auth)
	if err != nil {
		return "", err
	}
	oldCommit := ""
	if !cloned {
		if head, headErr := repository.Head(); headErr == nil {
			oldCommit = head.Hash().String()
		}
	}
	fetched := false
	if !cloned {
		if err := verifyOrigin(repository, request.Remote); err != nil {
			return "", err
		}
		head, headErr := repository.Head()
		if headErr != nil {
			return "", fmt.Errorf("mobile: read current branch: %w", headErr)
		}
		if !head.Name().IsBranch() || head.Name().Short() != request.Ref {
			return "", errors.New("mobile: changing the tracked branch requires a new repository session")
		}
		refspec := gitconfig.RefSpec("+refs/heads/" + request.Ref + ":refs/remotes/origin/" + request.Ref)
		err = repository.FetchContext(ctx, &git.FetchOptions{
			RemoteName: "origin",
			RefSpecs:   []gitconfig.RefSpec{refspec},
			Auth:       auth,
			Force:      true,
			Tags:       git.NoTags,
		})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return "", fmt.Errorf("mobile: fetch %s: %w", request.Ref, err)
		}
		fetched = true
		target, refErr := repository.Reference(plumbing.NewRemoteReferenceName("origin", request.Ref), true)
		if refErr != nil {
			return "", fmt.Errorf("mobile: resolve origin/%s: %w", request.Ref, refErr)
		}
		worktree, worktreeErr := repository.Worktree()
		if worktreeErr != nil {
			return "", fmt.Errorf("mobile: open git worktree: %w", worktreeErr)
		}
		if resetErr := worktree.Reset(&git.ResetOptions{Commit: target.Hash(), Mode: git.HardReset}); resetErr != nil {
			if rollbackErr := rollbackWorktree(worktree, oldCommit); rollbackErr != nil {
				c.invalidate()
				return "", fmt.Errorf("mobile: activate origin/%s: %w; rollback failed: %v", request.Ref, resetErr, rollbackErr)
			}
			return "", fmt.Errorf("mobile: activate origin/%s: %w", request.Ref, resetErr)
		}
	}

	head, err := repository.Head()
	if err != nil {
		return "", fmt.Errorf("mobile: read synced HEAD: %w", err)
	}
	newCommit := head.Hash().String()
	changed := oldCommit != newCommit
	previousTracked := c.tracked
	previousGitManaged := c.gitManaged
	c.gitManaged = true
	c.tracked, err = trackedFiles(repository)
	if err != nil {
		c.tracked = previousTracked
		c.gitManaged = previousGitManaged
		return "", fmt.Errorf("mobile: read git index: %w", err)
	}
	if c.repository == nil || changed {
		if err := c.reload(ctx); err != nil {
			if !cloned && oldCommit != "" {
				worktree, worktreeErr := repository.Worktree()
				if worktreeErr != nil {
					c.invalidate()
					return "", fmt.Errorf("mobile: activate synced notes: %w; open rollback worktree: %v", err, worktreeErr)
				}
				if rollbackErr := rollbackWorktree(worktree, oldCommit); rollbackErr != nil {
					c.invalidate()
					return "", fmt.Errorf("mobile: activate synced notes: %w; rollback failed: %v", err, rollbackErr)
				}
			}
			c.tracked = previousTracked
			c.gitManaged = previousGitManaged
			return "", fmt.Errorf("mobile: activate synced notes: %w", err)
		}
	}
	c.remote = request.Remote
	c.ref = request.Ref
	c.commit = newCommit
	return encodeJSON(syncResult{
		Cloned: cloned, Fetched: fetched, Changed: changed,
		OldCommit: oldCommit, NewCommit: newCommit, NoteCount: len(c.notes),
	})
}

func (c *Core) openOrClone(ctx context.Context, request syncRequest, auth transport.AuthMethod) (*git.Repository, bool, error) {
	repository, err := git.PlainOpen(c.repositoryPath)
	if err == nil {
		return repository, false, nil
	}
	if !errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, false, fmt.Errorf("mobile: open git repository: %w", err)
	}
	empty, err := directoryEmptyOrAbsent(c.repositoryPath)
	if err != nil {
		return nil, false, fmt.Errorf("mobile: inspect repository path: %w", err)
	}
	if !empty {
		return nil, false, errors.New("mobile: repository path is non-empty and is not a git repository")
	}
	if err := os.MkdirAll(filepath.Dir(c.repositoryPath), 0o755); err != nil {
		return nil, false, fmt.Errorf("mobile: create repository parent: %w", err)
	}
	repository, err = git.PlainCloneContext(ctx, c.repositoryPath, false, &git.CloneOptions{
		URL:           request.Remote,
		ReferenceName: plumbing.NewBranchReferenceName(request.Ref),
		SingleBranch:  true,
		Auth:          auth,
		Tags:          git.NoTags,
	})
	if err != nil {
		return nil, false, fmt.Errorf("mobile: clone %s: %w", request.Ref, err)
	}
	return repository, true, nil
}

func validateRemote(remote string) error {
	if filepath.IsAbs(remote) {
		return nil
	}
	parsed, err := url.Parse(remote)
	if err != nil {
		return fmt.Errorf("mobile: invalid remote: %w", err)
	}
	if parsed.User != nil {
		return errors.New("mobile: credentials must not be embedded in the remote URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "file" {
		return errors.New("mobile: remote must use HTTPS")
	}
	if parsed.Host == "" && parsed.Scheme == "https" {
		return errors.New("mobile: HTTPS remote is missing a host")
	}
	return nil
}

func gitAuth(username, token string) transport.AuthMethod {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if strings.TrimSpace(username) == "" {
		username = "x-access-token"
	}
	return &http.BasicAuth{Username: strings.TrimSpace(username), Password: token}
}

func (c *Core) loadGitStatus() {
	repository, err := git.PlainOpen(c.repositoryPath)
	if err != nil {
		return
	}
	c.gitManaged = true
	c.tracked, _ = trackedFiles(repository)
	if head, headErr := repository.Head(); headErr == nil {
		c.commit = head.Hash().String()
		if head.Name().IsBranch() {
			c.ref = head.Name().Short()
		}
	}
	if origin, originErr := repository.Remote("origin"); originErr == nil && len(origin.Config().URLs) > 0 {
		c.remote = origin.Config().URLs[0]
	}
}

func (c *Core) prepareGitWorkspace() error {
	repository, err := git.PlainOpen(c.repositoryPath)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mobile: open existing git repository: %w", err)
	}
	head, err := repository.Head()
	if err != nil {
		return fmt.Errorf("mobile: read existing git HEAD: %w", err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return fmt.Errorf("mobile: open existing git worktree: %w", err)
	}
	// A prior process may have stopped during reset. Materialize HEAD completely
	// before scanning so a mixed checkout can never become an active projection.
	if err := worktree.Reset(&git.ResetOptions{Commit: head.Hash(), Mode: git.HardReset}); err != nil {
		return fmt.Errorf("mobile: repair existing git worktree: %w", err)
	}
	c.loadGitStatus()
	return nil
}

func trackedFiles(repository *git.Repository) (map[string]struct{}, error) {
	index, err := repository.Storer.Index()
	if err != nil {
		return nil, err
	}
	tracked := make(map[string]struct{}, len(index.Entries))
	for _, entry := range index.Entries {
		tracked[filepath.ToSlash(entry.Name)] = struct{}{}
	}
	return tracked, nil
}

func rollbackWorktree(worktree *git.Worktree, commit string) error {
	if commit == "" {
		return errors.New("previous commit is unknown")
	}
	return worktree.Reset(&git.ResetOptions{Commit: plumbing.NewHash(commit), Mode: git.HardReset})
}

func (c *Core) invalidate() {
	if c.index != nil {
		_ = c.index.Close()
	}
	c.index = nil
	c.repository = nil
	c.notes = nil
	c.parsed = make(map[domain.NoteID]domain.ParsedOKFNote)
}

func verifyOrigin(repository *git.Repository, expected string) error {
	remote, err := repository.Remote("origin")
	if err != nil {
		return fmt.Errorf("mobile: open origin: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 || urls[0] != expected {
		return errors.New("mobile: sync remote does not match the repository origin")
	}
	return nil
}

func directoryEmptyOrAbsent(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
