package application

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/platform"
)

// An external edit to a note must reach the UI as note:updated so a note open in
// read mode can refresh itself.
func TestWatcherEmitsNoteUpdatedOnExternalEdit(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntitle: Alpha\n---\n\n# Alpha\noriginal body\n")

	var mu sync.Mutex
	var updated []NoteDTO
	service := testService(t, func(name string, payload any) {
		if name != "note:updated" {
			return
		}
		dto, ok := payload.(NoteDTO)
		if !ok {
			return
		}
		mu.Lock()
		updated = append(updated, dto)
		mu.Unlock()
	})

	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	// Let the watcher take its baseline snapshot before changing anything.
	time.Sleep(300 * time.Millisecond)

	const body = "---\ntitle: Alpha\n---\n\n# Alpha\nrewritten by another editor\n"
	if err := os.WriteFile(filepath.Join(root, "alpha.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	dto, ok := awaitNoteUpdate(t, &mu, &updated, "alpha")
	if !ok {
		t.Fatal("no note:updated emitted after an external edit")
	}
	if dto.Content != body {
		t.Fatalf("note:updated carried stale content:\n got %q\nwant %q", dto.Content, body)
	}
	if dto.Version == "" {
		t.Fatal("note:updated carried no version token; the client uses it to skip echoes")
	}
}

// A broken search/graph projection must not suppress the change notifications.
// Reading a note only needs the repository, so a projection failure should
// degrade search rather than freeze the open note on stale content.
func TestProjectionFailureStillEmitsNoteUpdated(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntitle: Alpha\n---\n\n# Alpha\noriginal body\n")

	var mu sync.Mutex
	var updated []NoteDTO
	service := testService(t, func(name string, payload any) {
		if name != "note:updated" {
			return
		}
		if dto, ok := payload.(NoteDTO); ok {
			mu.Lock()
			updated = append(updated, dto)
			mu.Unlock()
		}
	})

	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	// Break the projection the way a lost store would: close it underneath the
	// service so every write fails.
	service.mu.Lock()
	store := service.graphStore
	service.mu.Unlock()
	if store == nil {
		t.Fatal("expected an open graph store")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close graph store: %v", err)
	}

	const body = "---\ntitle: Alpha\n---\n\n# Alpha\nrewritten while the projection is broken\n"
	if err := os.WriteFile(filepath.Join(root, "alpha.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// processWorkspaceChanges is expected to report the projection failure, but
	// only after the content events have gone out.
	err := service.processWorkspaceChanges(ctx, changeSetFor("alpha"))
	if err == nil {
		t.Fatal("expected the projection failure to be reported")
	}

	dto, ok := awaitNoteUpdate(t, &mu, &updated, "alpha")
	if !ok {
		t.Fatal("projection failure suppressed note:updated; the open note would stay stale")
	}
	if dto.Content != body {
		t.Fatalf("note:updated carried stale content:\n got %q\nwant %q", dto.Content, body)
	}
}

func changeSetFor(ids ...string) platform.WorkspaceChangeSet {
	var set platform.WorkspaceChangeSet
	for _, id := range ids {
		set.Changed = append(set.Changed, domain.NoteID(id))
	}
	return set
}

// awaitNoteUpdate waits for a note:updated carrying the given id, returning the
// most recent one seen.
func awaitNoteUpdate(t *testing.T, mu *sync.Mutex, seen *[]NoteDTO, id string) (NoteDTO, bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		var found NoteDTO
		ok := false
		for _, dto := range *seen {
			if dto.ID == id {
				found = dto
				ok = true
			}
		}
		mu.Unlock()
		if ok {
			return found, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return NoteDTO{}, false
}
