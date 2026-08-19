package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecentWorkspacesHidesRootsMissingFromDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewRecentWorkspaceStore(filepath.Join(dir, "recent.json"), DefaultRecentWorkspaceLimit)
	live := t.TempDir()
	gone := filepath.Join(dir, "deleted-scratch-workspace")

	if err := store.write([]RecentWorkspace{
		{Path: gone, OpenedAt: time.Now().UTC()},
		{Path: live, OpenedAt: time.Now().UTC().Add(-time.Hour)},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only the workspace that exists on disk, got %d: %+v", len(items), items)
	}
	if !samePath(items[0].Path, live) {
		t.Fatalf("expected %s, got %s", live, items[0].Path)
	}

	// A file is not a workspace root either.
	notADir := filepath.Join(dir, "note.md")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if RootExists(notADir) {
		t.Fatal("a regular file should not count as an existing workspace root")
	}
}

// The recent file is capped, so dead entries must not occupy the slots real
// workspaces need — that is exactly how a run of throwaway agent workspaces
// pushes the workspaces someone actually uses out of the menu.
func TestRecentWorkspacesEvictMissingRootsBeforeLiveOnes(t *testing.T) {
	dir := t.TempDir()
	store := NewRecentWorkspaceStore(filepath.Join(dir, "recent.json"), 3)

	keep := t.TempDir()
	seed := []RecentWorkspace{
		{Path: filepath.Join(dir, "scratch-1"), OpenedAt: time.Now().UTC()},
		{Path: filepath.Join(dir, "scratch-2"), OpenedAt: time.Now().UTC()},
		{Path: keep, OpenedAt: time.Now().UTC().Add(-time.Hour)},
	}
	if err := store.write(seed); err != nil {
		t.Fatalf("write: %v", err)
	}

	opened := t.TempDir()
	if err := store.Add(context.Background(), opened); err != nil {
		t.Fatalf("add: %v", err)
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected both live workspaces to survive, got %d: %+v", len(items), items)
	}
	if !samePath(items[0].Path, opened) {
		t.Fatalf("expected the just-opened workspace first, got %s", items[0].Path)
	}
	if !samePath(items[1].Path, keep) {
		t.Fatalf("expected the older live workspace to be retained, got %s", items[1].Path)
	}
}

// Vanishing is not the same as being forgotten: an unmounted volume should come
// back when it is remounted, so long as it is not crowding anything out.
func TestRecentWorkspacesRetainMissingRootsWhenThereIsRoom(t *testing.T) {
	dir := t.TempDir()
	store := NewRecentWorkspaceStore(filepath.Join(dir, "recent.json"), DefaultRecentWorkspaceLimit)
	unmounted := filepath.Join(dir, "volume-that-went-away")
	if err := store.write([]RecentWorkspace{{Path: unmounted, OpenedAt: time.Now().UTC()}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := store.Add(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("add: %v", err)
	}

	stored, err := store.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	found := false
	for _, item := range stored {
		if samePath(item.Path, unmounted) {
			found = true
		}
	}
	if !found {
		t.Fatal("a missing root should be retained in the file while there is room for it")
	}

	listed, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, item := range listed {
		if samePath(item.Path, unmounted) {
			t.Fatal("a missing root must still be hidden from the menu")
		}
	}
}
