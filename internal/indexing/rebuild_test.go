package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/search"
)

func TestRebuildBuildsSearchGraphAndState(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\ntags: [go]\n---\n\n# Beta\n")
	var progress []RebuildProgress
	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow, Progress: func(p RebuildProgress) { progress = append(progress, p) }}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if result.TotalNotes != 2 || result.ParsedNotes != 2 || result.FailedNotes != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !hasStage(progress, ProgressComplete) {
		t.Fatalf("expected complete progress, got %#v", progress)
	}
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatalf("open search: %v", err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 || results[0].ID != "alpha" {
		t.Fatalf("unexpected search results: %#v", results)
	}
	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("unexpected backlinks: %#v", backlinks)
	}
	state, err := ReadState(result.StatePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.ParsedNotes != 2 || state.SearchPath != result.SearchPath || state.GraphPath != result.GraphPath {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestRebuildRemovesStaleSearchAndGraphEntriesAfterDelete(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n[Beta](beta.md)\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\nBeta body\n")
	rebuilder := Rebuilder{WorkerCount: 1, Now: fixedNow}
	if _, err := rebuilder.Rebuild(context.Background(), root); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "beta.md")); err != nil {
		t.Fatal(err)
	}
	result, err := rebuilder.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "Beta", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.ID == "beta" {
			t.Fatalf("stale beta search result remained: %#v", results)
		}
	}
	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 0 {
		t.Fatalf("stale beta backlink remained: %#v", backlinks)
	}
}

func TestRebuildCollapsesDuplicateMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n\n## Links\n[Beta](beta.md) and [Beta again](beta.md)\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\n# Beta\n")
	result, err := Rebuilder{WorkerCount: 1, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild duplicate links: %v", err)
	}
	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("expected duplicate markdown links to collapse, got %#v", backlinks)
	}
}
func TestRebuildContinuesAfterPartialParseFailures(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "good.md", "---\ntype: concept\ntitle: Good\n---\n\nGood body\n")
	writeNote(t, root, "bad.md", "---\ntitle: Missing type\n---\n\nBad body\n")
	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild should tolerate parse failure: %v", err)
	}
	if result.TotalNotes != 2 || result.ParsedNotes != 1 || result.FailedNotes != 1 {
		t.Fatalf("unexpected partial result: %#v", result)
	}
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "Good", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != "good" {
		t.Fatalf("good note was not indexed: %#v", results)
	}
}

func TestRebuildHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Rebuilder{WorkerCount: 1}.Rebuild(ctx, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func writeNote(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
}

func hasStage(progress []RebuildProgress, stage ProgressStage) bool {
	for _, item := range progress {
		if item.Stage == stage {
			return true
		}
	}
	return false
}
