package application

import (
	"context"
	"fmt"
	"testing"
)

// TestListNotesServedFromSQLiteWithTags verifies the note list comes from the
// SQLite projection (tags populated — previously always empty) after open.
func TestListNotesServedFromSQLiteWithTags(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go, db]\n---\n\n# Alpha\n")
	writeNote(t, root, "notes/beta.md", "---\ntype: concept\ntitle: Beta\ntags: [ui]\n---\n\n# Beta\n")
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	notes, err := service.ListNotes(ctx)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d: %#v", len(notes), notes)
	}
	byID := map[string]NoteSummaryDTO{}
	for _, n := range notes {
		byID[n.ID] = n
	}
	alpha, ok := byID["alpha"]
	if !ok {
		t.Fatalf("alpha missing: %#v", notes)
	}
	// Tags are now populated from the SQLite projection.
	if len(alpha.Tags) != 2 {
		t.Fatalf("expected alpha tags [db go], got %#v", alpha.Tags)
	}
	// Title stays the filename-derived form (stable link resolution).
	if alpha.Title != "alpha" {
		t.Fatalf("expected filename-derived title 'alpha', got %q", alpha.Title)
	}
	beta, ok := byID["notes/beta"]
	if !ok {
		t.Fatalf("notes/beta missing: %#v", notes)
	}
	if beta.Path != "notes/beta.md" {
		t.Fatalf("unexpected beta path %q", beta.Path)
	}
}

func TestListNotesPagePaginatesAndFilters(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		tag := "even"
		if i%2 == 1 {
			tag = "odd"
		}
		writeNote(t, root, fmt.Sprintf("n%02d.md", i),
			fmt.Sprintf("---\ntype: concept\ntitle: Note %02d\ntags: [%s]\n---\n\n# Note %02d\n", i, tag, i))
	}
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	page, err := service.ListNotesPage(ctx, ListNotesQueryDTO{Limit: 5, Offset: 0, SortBy: "id"})
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if page.Total != 12 {
		t.Fatalf("expected total 12, got %d", page.Total)
	}
	if len(page.Items) != 5 || page.Items[0].ID != "n00" {
		t.Fatalf("unexpected first page: %#v", page.Items)
	}

	second, err := service.ListNotesPage(ctx, ListNotesQueryDTO{Limit: 5, Offset: 5, SortBy: "id"})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(second.Items) != 5 || second.Items[0].ID != "n05" {
		t.Fatalf("unexpected second page: %#v", second.Items)
	}

	filtered, err := service.ListNotesPage(ctx, ListNotesQueryDTO{Tag: "odd"})
	if err != nil {
		t.Fatalf("tag page: %v", err)
	}
	if filtered.Total != 6 || len(filtered.Items) != 6 {
		t.Fatalf("expected 6 odd notes, got total=%d items=%d", filtered.Total, len(filtered.Items))
	}
}
