package graph

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/domain"
)

func metaFixture(id, title, path string, tags ...domain.Tag) NoteMeta {
	return NoteMeta{ID: domain.NoteID(id), Title: title, Path: path, ModifiedAt: time.Unix(1_700_000_000, 0).UTC(), Tags: tags}
}

func TestUpsertNoteMetaAndListNotes(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	defer store.Close()

	if err := store.UpsertNoteMeta(ctx, metaFixture("beta", "Beta", "beta.md", "go", "db")); err != nil {
		t.Fatalf("upsert beta: %v", err)
	}
	if err := store.UpsertNoteMeta(ctx, metaFixture("alpha", "Alpha", "alpha.md", "go")); err != nil {
		t.Fatalf("upsert alpha: %v", err)
	}
	if err := store.UpsertNoteMeta(ctx, metaFixture("gamma", "Gamma", "gamma.md")); err != nil {
		t.Fatalf("upsert gamma: %v", err)
	}

	res, err := store.ListNotes(ctx, ListNotesOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Total != 3 || len(res.Items) != 3 {
		t.Fatalf("expected 3 notes, got total=%d items=%d", res.Total, len(res.Items))
	}
	// Default sort is by id ascending.
	if res.Items[0].ID != "alpha" || res.Items[1].ID != "beta" || res.Items[2].ID != "gamma" {
		t.Fatalf("unexpected default order: %#v", res.Items)
	}
	if res.Items[1].Title != "Beta" || res.Items[1].Path != "beta.md" {
		t.Fatalf("unexpected beta row: %#v", res.Items[1])
	}
	if len(res.Items[1].Tags) != 2 || res.Items[1].Tags[0] != "db" || res.Items[1].Tags[1] != "go" {
		t.Fatalf("unexpected beta tags: %#v", res.Items[1].Tags)
	}
	if res.Items[1].ModifiedAt == "" {
		t.Fatalf("expected modified_at to be set")
	}
}

func TestListNotesSortFilterPaginate(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	defer store.Close()
	for _, m := range []NoteMeta{
		metaFixture("a", "Charlie", "a.md", "go"),
		metaFixture("b", "alpha", "b.md", "go", "db"),
		metaFixture("c", "Bravo", "c.md", "db"),
		metaFixture("d", "delta", "d.md"),
	} {
		if err := store.UpsertNoteMeta(ctx, m); err != nil {
			t.Fatalf("upsert %s: %v", m.ID, err)
		}
	}

	// Title sort is case-insensitive: alpha, Bravo, Charlie, delta.
	byTitle, err := store.ListNotes(ctx, ListNotesOptions{SortBy: "title"})
	if err != nil {
		t.Fatalf("title sort: %v", err)
	}
	gotTitles := []string{byTitle.Items[0].Title, byTitle.Items[1].Title, byTitle.Items[2].Title, byTitle.Items[3].Title}
	want := []string{"alpha", "Bravo", "Charlie", "delta"}
	for i := range want {
		if gotTitles[i] != want[i] {
			t.Fatalf("title sort mismatch at %d: got %v want %v", i, gotTitles, want)
		}
	}

	// Tag filter narrows the total and the page.
	byTag, err := store.ListNotes(ctx, ListNotesOptions{Tag: "go"})
	if err != nil {
		t.Fatalf("tag filter: %v", err)
	}
	if byTag.Total != 2 || len(byTag.Items) != 2 {
		t.Fatalf("expected 2 go-tagged notes, got total=%d items=%d", byTag.Total, len(byTag.Items))
	}

	// Pagination: total reflects the whole set, items only the window.
	page, err := store.ListNotes(ctx, ListNotesOptions{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("paginate: %v", err)
	}
	if page.Total != 4 {
		t.Fatalf("expected total 4, got %d", page.Total)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "b" || page.Items[1].ID != "c" {
		t.Fatalf("unexpected page: %#v", page.Items)
	}

	// Search matches title/id substring, case-insensitively.
	search, err := store.ListNotes(ctx, ListNotesOptions{Search: "RAV"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if search.Total != 1 || search.Items[0].ID != "c" {
		t.Fatalf("unexpected search result: %#v", search.Items)
	}
}

func TestUpsertNoteMetaReplacesTags(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	defer store.Close()
	if err := store.UpsertNoteMeta(ctx, metaFixture("n", "N", "n.md", "one", "two")); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNoteMeta(ctx, metaFixture("n", "N2", "n.md", "three")); err != nil {
		t.Fatal(err)
	}
	res, err := store.ListNotes(ctx, ListNotesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Title != "N2" {
		t.Fatalf("expected updated title, got %#v", res.Items)
	}
	if len(res.Items[0].Tags) != 1 || res.Items[0].Tags[0] != "three" {
		t.Fatalf("expected tags replaced to [three], got %#v", res.Items[0].Tags)
	}
}

func TestDeleteNoteClearsMetaAndTags(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	defer store.Close()
	if err := store.UpsertNoteMeta(ctx, metaFixture("n", "N", "n.md", "one")); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNote(ctx, "n"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	res, err := store.ListNotes(ctx, ListNotesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 || len(res.Items) != 0 {
		t.Fatalf("expected empty after delete, got %#v", res)
	}
	// note_tags row must be gone too (FK cascade).
	var tagCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM note_tags`).Scan(&tagCount); err != nil {
		t.Fatal(err)
	}
	if tagCount != 0 {
		t.Fatalf("expected note_tags cleared, got %d", tagCount)
	}
}

// TestUpsertNoteMetaRoundTripsTypeAndFilters verifies that a note's type is
// persisted, returned in the list rows, and usable as an exact case-insensitive
// filter — taxonomy-agnostic (whatever string the note's type frontmatter holds).
func TestUpsertNoteMetaRoundTripsTypeAndFilters(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	defer store.Close()

	concept := metaFixture("a", "A", "a.md")
	concept.Type = "concept"
	procedure := metaFixture("b", "B", "b.md")
	procedure.Type = "procedure"
	untyped := metaFixture("c", "C", "c.md") // Type == ""
	for _, m := range []NoteMeta{concept, procedure, untyped} {
		if err := store.UpsertNoteMeta(ctx, m); err != nil {
			t.Fatalf("upsert %s: %v", m.ID, err)
		}
	}

	all, err := store.ListNotes(ctx, ListNotesOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all.Items) != 3 {
		t.Fatalf("expected 3 notes, got %#v", all.Items)
	}
	if all.Items[0].Type != "concept" || all.Items[1].Type != "procedure" || all.Items[2].Type != "" {
		t.Fatalf("unexpected types round-tripped: %#v", all.Items)
	}

	// Exact type filter narrows the total and page.
	byType, err := store.ListNotes(ctx, ListNotesOptions{Type: "procedure"})
	if err != nil {
		t.Fatalf("type filter: %v", err)
	}
	if byType.Total != 1 || len(byType.Items) != 1 || byType.Items[0].ID != "b" {
		t.Fatalf("expected only b for type=procedure, got %#v", byType)
	}

	// Filter is case-insensitive.
	byTypeUpper, err := store.ListNotes(ctx, ListNotesOptions{Type: "CONCEPT"})
	if err != nil {
		t.Fatalf("type filter (upper): %v", err)
	}
	if byTypeUpper.Total != 1 || byTypeUpper.Items[0].ID != "a" {
		t.Fatalf("expected case-insensitive match for a, got %#v", byTypeUpper)
	}
}

// TestListNotesExcludesLinkOnlyRows verifies that a resolved-target id inserted
// with no metadata (path == ”) does not appear in the note list until it gets
// its own meta.
func TestListNotesExcludesLinkOnlyRows(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	defer store.Close()
	target := domain.NoteID("target")
	// "source" links to "target"; ReplaceOutgoingLinks inserts a bare target row.
	if err := store.ReplaceOutgoingLinks(ctx, "source", []domain.NoteLink{
		{Target: "target", ResolvedID: &target, Strength: domain.LinkStrengthHard},
	}); err != nil {
		t.Fatal(err)
	}
	// Only "source" has metadata.
	if err := store.UpsertNoteMeta(ctx, metaFixture("source", "Source", "source.md")); err != nil {
		t.Fatal(err)
	}
	res, err := store.ListNotes(ctx, ListNotesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Items[0].ID != "source" {
		t.Fatalf("expected only source listed, got %#v", res.Items)
	}
}
