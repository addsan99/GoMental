package search

import (
	"context"
	"path/filepath"
	"testing"

	"GoMental/internal/domain"
)

func TestBleveIndexSearchRanksTitleAboveBodyAndHighlights(t *testing.T) {
	idx := openTestIndex(t)
	ctx := context.Background()
	indexDocs(t, idx,
		doc("title-hit", "alpha.md", "Alpha Systems", "plain body", []domain.Tag{"systems"}),
		doc("body-hit", "body.md", "Other", "Alpha appears in body many times alpha alpha", []domain.Tag{"systems"}),
	)
	results, err := idx.Search(ctx, domain.SearchQuery{Text: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected two results, got %#v", results)
	}
	if results[0].ID != "title-hit" {
		t.Fatalf("expected title result first, got %#v", results)
	}
	if len(results[0].Fragments) == 0 {
		t.Fatalf("expected highlight fragments, got %#v", results[0])
	}
}

func TestBleveIndexFiltersByTagAndPath(t *testing.T) {
	idx := openTestIndex(t)
	ctx := context.Background()
	indexDocs(t, idx,
		doc("notes/alpha", "notes/alpha.md", "Alpha", "body", []domain.Tag{"go"}),
		doc("notes/beta", "notes/beta.md", "Beta", "body", []domain.Tag{"okf"}),
		doc("archive/alpha", "archive/alpha.md", "Alpha Archive", "body", []domain.Tag{"go"}),
	)
	results, err := idx.Search(ctx, domain.SearchQuery{Text: "alpha", Tags: []domain.Tag{"go"}, PathPrefix: "notes/", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "notes/alpha" {
		t.Fatalf("unexpected filtered results: %#v", results)
	}
}

func TestBleveIndexDeleteAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index")
	idx, err := OpenBleveIndex(path)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	ctx := context.Background()
	indexDocs(t, idx, doc("alpha", "alpha.md", "Alpha", "body", nil))
	if err := idx.Delete(ctx, "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := OpenBleveIndex(path)
	if err != nil {
		t.Fatalf("reopen index: %v", err)
	}
	defer reopened.Close()
	results, err := reopened.Search(ctx, domain.SearchQuery{Text: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search reopened: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected deleted doc to stay deleted, got %#v", results)
	}
}

func TestBleveIndexRebuildReplacesProjection(t *testing.T) {
	idx := openTestIndex(t)
	ctx := context.Background()
	indexDocs(t, idx, doc("old", "old.md", "Old", "body", nil))
	if err := idx.Rebuild(ctx, []domain.SearchDocument{doc("new", "new.md", "New Alpha", "body", nil)}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	results, err := idx.Search(ctx, domain.SearchQuery{Text: "new", Limit: 10})
	if err != nil {
		t.Fatalf("search new: %v", err)
	}
	if len(results) != 1 || results[0].ID != "new" {
		t.Fatalf("unexpected rebuild results: %#v", results)
	}
	old, err := idx.Search(ctx, domain.SearchQuery{Text: "old", Limit: 10})
	if err != nil {
		t.Fatalf("search old: %v", err)
	}
	if len(old) != 0 {
		t.Fatalf("expected old projection removed, got %#v", old)
	}
}

func openTestIndex(t *testing.T) *BleveIndex {
	t.Helper()
	idx, err := OpenBleveIndex(filepath.Join(t.TempDir(), "index"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func indexDocs(t *testing.T, idx *BleveIndex, docs ...domain.SearchDocument) {
	t.Helper()
	for _, document := range docs {
		if err := idx.Index(context.Background(), document); err != nil {
			t.Fatalf("index %s: %v", document.ID, err)
		}
	}
}

func doc(id domain.NoteID, path string, title string, body string, tags []domain.Tag) domain.SearchDocument {
	return domain.SearchDocument{
		ID:       id,
		Path:     domain.NotePath(path),
		Title:    title,
		Body:     body,
		Headings: []string{title},
		Tags:     tags,
	}
}
