package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultRecentWorkspaceLimit = 10

type RecentWorkspace struct {
	Path     string    `json:"path"`
	OpenedAt time.Time `json:"openedAt"`
}

type RecentWorkspaceStore struct {
	path  string
	limit int
}

func NewRecentWorkspaceStore(path string, limit int) RecentWorkspaceStore {
	if limit <= 0 {
		limit = DefaultRecentWorkspaceLimit
	}
	return RecentWorkspaceStore{path: path, limit: limit}
}

func DefaultRecentWorkspaceStore() (RecentWorkspaceStore, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return RecentWorkspaceStore{}, err
	}
	return NewRecentWorkspaceStore(filepath.Join(configDir, "GoMental", "recent-workspaces.json"), DefaultRecentWorkspaceLimit), nil
}

// List returns the recent workspaces that still resolve to a directory on disk.
// Entries whose root has since been deleted are filtered out rather than
// removed from the file: a workspace on an unmounted volume should reappear
// once the volume is back, and offering a menu entry that cannot be opened is
// worse than briefly hiding one that can.
func (s RecentWorkspaceStore) List(ctx context.Context) ([]RecentWorkspace, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	items, err := s.read()
	if err != nil {
		return nil, err
	}
	live := make([]RecentWorkspace, 0, len(items))
	for _, item := range items {
		if RootExists(item.Path) {
			live = append(live, item)
		}
	}
	return live, nil
}

// RootExists reports whether a workspace root is still a directory on disk.
func RootExists(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

func (s RecentWorkspaceStore) Add(ctx context.Context, root string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	ws, err := Open(root)
	if err != nil {
		return err
	}
	items, err := s.read()
	if err != nil {
		return err
	}
	canonical := ws.Root()
	updated := []RecentWorkspace{{Path: canonical, OpenedAt: time.Now().UTC()}}
	// Entries whose root is gone are kept, but only in slots the live entries do
	// not want. Without this an agent's handful of deleted scratch workspaces
	// would sit at the head of the file and evict every real one.
	var missing []RecentWorkspace
	for _, item := range items {
		if samePath(item.Path, canonical) {
			continue
		}
		if !RootExists(item.Path) {
			missing = append(missing, item)
			continue
		}
		updated = append(updated, item)
		if len(updated) >= s.limit {
			break
		}
	}
	for _, item := range missing {
		if len(updated) >= s.limit {
			break
		}
		updated = append(updated, item)
	}
	return s.write(updated)
}

func (s RecentWorkspaceStore) read() ([]RecentWorkspace, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var items []RecentWorkspace
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s RecentWorkspaceStore) write(items []RecentWorkspace) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0o644)
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
