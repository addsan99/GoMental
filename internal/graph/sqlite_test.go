package graph

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/domain"
)

func TestSQLiteStoreReplaceOutgoingBacklinksAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.sqlite")
	store := openStore(t, path)
	ctx := context.Background()
	beta := domain.NoteID("beta")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{
		{Target: "beta", ResolvedID: &beta, DisplayText: "Beta", Strength: domain.LinkStrengthHard},
		{Target: "missing", DisplayText: "Missing", Strength: domain.LinkStrengthHard},
	}); err != nil {
		t.Fatalf("replace outgoing: %v", err)
	}
	outgoing, err := store.OutgoingLinks(ctx, "alpha")
	if err != nil {
		t.Fatalf("outgoing: %v", err)
	}
	if len(outgoing) != 2 {
		t.Fatalf("expected two outgoing links, got %#v", outgoing)
	}
	backlinks, err := store.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("unexpected backlinks: %#v", backlinks)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened := openStore(t, path)
	backlinks, err = reopened.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("reopened backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("unexpected reopened backlinks: %#v", backlinks)
	}
}

func TestSQLiteStoreReplacementAndDelete(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()
	beta := domain.NoteID("beta")
	gamma := domain.NoteID("gamma")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{{Target: "beta", ResolvedID: &beta, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{{Target: "gamma", ResolvedID: &gamma, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	outgoing, err := store.OutgoingLinks(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(outgoing) != 1 || *outgoing[0].ResolvedID != "gamma" {
		t.Fatalf("replacement failed: %#v", outgoing)
	}
	if err := store.DeleteNote(ctx, "gamma"); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	backlinks, err := store.Backlinks(ctx, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 0 {
		t.Fatalf("expected gamma backlinks deleted, got %#v", backlinks)
	}
}

func TestSQLiteStoreCollapsesDuplicateLinks(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()
	beta := domain.NoteID("beta")
	duplicateHard := []domain.NoteLink{
		{Target: "beta.md", ResolvedID: &beta, DisplayText: "Beta", Heading: "Links", Strength: domain.LinkStrengthHard},
		{Target: "beta.md", ResolvedID: &beta, DisplayText: "Beta again", Heading: "Links", Strength: domain.LinkStrengthHard},
	}
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", duplicateHard); err != nil {
		t.Fatalf("replace duplicate hard links: %v", err)
	}
	backlinks, err := store.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 {
		t.Fatalf("expected duplicate hard links to collapse, got %#v", backlinks)
	}
	computedAt := time.Now()
	duplicateSoft := []domain.InferredNoteLink{
		{Source: "alpha", Target: "beta", Score: 0.8, Algorithm: "test", ComputedAt: computedAt},
		{Source: "alpha", Target: "beta", Score: 0.7, Algorithm: "test", ComputedAt: computedAt},
	}
	if err := store.ReplaceInferredLinks(ctx, "alpha", duplicateSoft); err != nil {
		t.Fatalf("replace duplicate soft links: %v", err)
	}
	full, err := store.FullGraph(ctx, domain.GraphFilter{IncludeSoftLinks: true})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	softCount := 0
	for _, edge := range full.Edges {
		if edge.Kind == domain.GraphEdgeInferredRelatedTo && edge.Source == "alpha" && edge.Target == "beta" {
			softCount++
		}
	}
	if softCount != 1 {
		t.Fatalf("expected duplicate soft links to collapse, got %#v", full.Edges)
	}
}
func TestSQLiteStoreFullGraphNeighborhoodAndSoftLinks(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()
	beta := domain.NoteID("beta")
	gamma := domain.NoteID("gamma")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{
		{Target: "beta", ResolvedID: &beta, Strength: domain.LinkStrengthHard},
		{Target: "missing", Strength: domain.LinkStrengthHard},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOutgoingLinks(ctx, "beta", []domain.NoteLink{{Target: "gamma", ResolvedID: &gamma, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceInferredLinks(ctx, "alpha", []domain.InferredNoteLink{{Source: "alpha", Target: "gamma", Score: 0.8, Algorithm: "test", ComputedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}

	full, err := store.FullGraph(ctx, domain.GraphFilter{IncludeUnresolved: true, IncludeSoftLinks: true})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if !hasEdgeKind(full, domain.GraphEdgeLinksTo) || !hasEdgeKind(full, domain.GraphEdgeInferredRelatedTo) || !hasNode(full, "unresolved:missing") {
		t.Fatalf("unexpected full graph: %#v", full)
	}
	neighborhood, err := store.Neighborhood(ctx, "alpha", 1)
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	if !hasNode(neighborhood, "beta") || !hasNode(neighborhood, "gamma") {
		t.Fatalf("expected alpha neighborhood to include beta and soft-linked gamma: %#v", neighborhood)
	}
	filtered, err := store.FullGraph(ctx, domain.GraphFilter{PathPrefix: "beta/", IncludeHardLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Edges) != 0 {
		t.Fatalf("expected path filter to remove all edges, got %#v", filtered.Edges)
	}
}

func TestSQLiteStoreFullGraphIncludesNotesWithoutEdges(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()
	if err := store.ReplaceAllLinks(ctx, []LinkProjection{
		{Source: "alpha"},
		{Source: "folder/beta"},
	}); err != nil {
		t.Fatalf("replace all links: %v", err)
	}
	full, err := store.FullGraph(ctx, domain.GraphFilter{IncludeUnresolved: true, IncludeSoftLinks: true})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if !hasNode(full, "alpha") || !hasNode(full, "folder/beta") {
		t.Fatalf("expected isolated note nodes in full graph, got %#v", full.Nodes)
	}
	filtered, err := store.FullGraph(ctx, domain.GraphFilter{PathPrefix: "folder/"})
	if err != nil {
		t.Fatalf("filtered full graph: %v", err)
	}
	if hasNode(filtered, "alpha") || !hasNode(filtered, "folder/beta") {
		t.Fatalf("expected path-filtered isolated notes, got %#v", filtered.Nodes)
	}
}

func openStore(t *testing.T, path string) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func hasNode(graph domain.Graph, id string) bool {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func hasEdgeKind(graph domain.Graph, kind domain.GraphEdgeKind) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			return true
		}
	}
	return false
}
