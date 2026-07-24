package application

import (
	"context"
	"errors"
	"testing"
)

// TestSaveNoteOptimisticConcurrency is the Phase 19 acceptance criterion: two
// concurrent saves sharing the same base version — one succeeds, the other is
// rejected with edit.external_conflict.
func TestSaveNoteOptimisticConcurrency(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n")
	service := testService(t, func(string, any) {})
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open: %v", err)
	}
	read, err := service.ReadNote(ctx, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Version == "" {
		t.Fatal("expected a version token on read")
	}

	results := make(chan error, 2)
	save := func(content string) {
		_, e := service.SaveNote(ctx, SaveNoteRequest{ID: "alpha", Content: content, BaseVersion: read.Version})
		results <- e
	}
	// Different content lengths guarantee the size component of the version
	// differs after the first write, so the check is robust to coarse mtime.
	go save("---\ntype: concept\ntitle: Alpha\n---\n\n# One\n")
	go save("---\ntype: concept\ntitle: Alpha\n---\n\n# Two — deliberately longer body\n")

	oks, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			oks++
		default:
			var appErr AppError
			if errors.As(err, &appErr) && appErr.Code == ErrExternalConflict {
				conflicts++
			} else {
				t.Fatalf("unexpected save error: %v", err)
			}
		}
	}
	if oks != 1 || conflicts != 1 {
		t.Fatalf("expected exactly 1 success and 1 conflict, got ok=%d conflict=%d", oks, conflicts)
	}
}

// TestSaveNoteStaleVersionConflicts verifies a save with a superseded base
// version is rejected, and that force / empty-base saves still win.
func TestSaveNoteStaleVersionConflicts(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n")
	service := testService(t, func(string, any) {})
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open: %v", err)
	}
	read, err := service.ReadNote(ctx, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	staleVersion := read.Version

	// First save with the correct base succeeds and advances the version.
	if _, err := service.SaveNote(ctx, SaveNoteRequest{ID: "alpha", Content: "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha v2 longer\n", BaseVersion: staleVersion}); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Second save with the now-stale base is rejected.
	_, err = service.SaveNote(ctx, SaveNoteRequest{ID: "alpha", Content: "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha v3\n", BaseVersion: staleVersion})
	var appErr AppError
	if !errors.As(err, &appErr) || appErr.Code != ErrExternalConflict {
		t.Fatalf("expected external_conflict, got %v", err)
	}

	// Force overrides the check.
	if _, err := service.SaveNote(ctx, SaveNoteRequest{ID: "alpha", Content: "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha forced\n", BaseVersion: staleVersion, Force: true}); err != nil {
		t.Fatalf("forced save: %v", err)
	}

	// Empty base (desktop default) is unconditional.
	if _, err := service.SaveNote(ctx, SaveNoteRequest{ID: "alpha", Content: "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha default\n"}); err != nil {
		t.Fatalf("unconditional save: %v", err)
	}
}
