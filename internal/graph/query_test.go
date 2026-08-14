package graph

import (
	"context"
	"path/filepath"
	"testing"

	"GoMental/internal/domain"
)

func idPtr(s string) *domain.NoteID {
	v := domain.NoteID(s)
	return &v
}

// Query restricts the note node set by the metadata predicates (type / tag),
// keeping only edges whose endpoints both survive the filter.
func TestQueryMetadataPredicateFiltering(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()

	// Chain alpha -> beta -> gamma; alpha/gamma are adr, beta is concept.
	beta := domain.NoteID("beta")
	gamma := domain.NoteID("gamma")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{{Target: "beta", ResolvedID: &beta, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOutgoingLinks(ctx, "beta", []domain.NoteLink{{Target: "gamma", ResolvedID: &gamma, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []NoteMeta{
		{ID: "alpha", Type: "adr", Tags: []domain.Tag{"arch"}},
		{ID: "beta", Type: "concept", Tags: []domain.Tag{"arch"}},
		{ID: "gamma", Type: "adr", Tags: []domain.Tag{"misc"}},
	} {
		if err := store.UpsertNoteMeta(ctx, m); err != nil {
			t.Fatalf("upsert meta %s: %v", m.ID, err)
		}
	}

	// Type predicate keeps only the adr notes. Both chain edges touch the
	// filtered-out concept note, so none survive.
	byType, err := store.Query(ctx, domain.GraphQuery{Types: []string{"adr"}})
	if err != nil {
		t.Fatalf("query types: %v", err)
	}
	if !hasNode(byType, "alpha") || !hasNode(byType, "gamma") || hasNode(byType, "beta") {
		t.Fatalf("type predicate should keep only adr notes, got %#v", byType.Nodes)
	}
	if len(byType.Edges) != 0 {
		t.Fatalf("expected no edges among adr-only notes, got %#v", byType.Edges)
	}

	// Tag predicate keeps the arch-tagged notes and their connecting hard edge.
	byTag, err := store.Query(ctx, domain.GraphQuery{Tags: []domain.Tag{"arch"}})
	if err != nil {
		t.Fatalf("query tags: %v", err)
	}
	if !hasNode(byTag, "alpha") || !hasNode(byTag, "beta") || hasNode(byTag, "gamma") {
		t.Fatalf("tag predicate should keep only arch notes, got %#v", byTag.Nodes)
	}
	if !hasEdgeKind(byTag, domain.GraphEdgeLinksTo) {
		t.Fatalf("expected the alpha->beta hard edge to survive, got %#v", byTag.Edges)
	}
}

// A seeded Query returns metadata (facet-hub) edges when IncludeMetadataLinks is
// set — closing the gap where the legacy Neighborhood never surfaces them.
func TestQueryNeighborhoodIncludesMetadataEdges(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()

	beta := domain.NoteID("beta")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{{Target: "beta", ResolvedID: &beta, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	alphaNote := metaNote("alpha", "concept", []domain.Tag{"go"}, nil)
	if err := store.ReplaceMetadataLinks(ctx, "alpha", MetadataMemberships(alphaNote)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNoteMeta(ctx, NoteMeta{ID: "alpha", Type: "concept", Tags: []domain.Tag{"go"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNoteMeta(ctx, NoteMeta{ID: "beta", Type: "concept"}); err != nil {
		t.Fatal(err)
	}

	// Legacy path: no metadata edges.
	legacy, err := store.Neighborhood(ctx, "alpha", 1)
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	if hasEdgeKind(legacy, domain.GraphEdgeTaggedWith) {
		t.Fatalf("legacy neighborhood should not surface metadata edges: %#v", legacy.Edges)
	}

	// Reworked path: metadata edge + hub node appear alongside the hard neighbour.
	g, err := store.Query(ctx, domain.GraphQuery{Seed: idPtr("alpha"), Depth: 1, IncludeMetadataLinks: true})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !hasEdgeKind(g, domain.GraphEdgeTaggedWith) {
		t.Fatalf("expected tagged_with edge in seeded query, got %#v", g.Edges)
	}
	if !hasNode(g, "tag:go") {
		t.Fatalf("expected tag hub node, got %#v", g.Nodes)
	}
	if !hasNode(g, "beta") {
		t.Fatalf("expected hard-linked beta in neighborhood, got %#v", g.Nodes)
	}
}

func TestQueryMetadataSeedExpandsThroughNoteLinksByDepth(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()

	beta := domain.NoteID("beta")
	gamma := domain.NoteID("gamma")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{{Target: "beta", ResolvedID: &beta, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOutgoingLinks(ctx, "beta", []domain.NoteLink{{Target: "gamma", ResolvedID: &gamma, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []domain.ParsedOKFNote{
		metaNote("alpha", "concept", []domain.Tag{"go"}, nil),
		metaNote("beta", "concept", nil, nil),
		metaNote("gamma", "concept", []domain.Tag{"go"}, nil),
	} {
		if err := store.ReplaceMetadataLinks(ctx, n.ID, MetadataMemberships(n)); err != nil {
			t.Fatalf("replace metadata %s: %v", n.ID, err)
		}
		if err := store.UpsertNoteMeta(ctx, NoteMeta{ID: n.ID, Type: n.Metadata.Type, Tags: n.Tags}); err != nil {
			t.Fatalf("upsert meta %s: %v", n.ID, err)
		}
	}

	depth1, err := store.Query(ctx, domain.GraphQuery{MetadataSeed: "tag:go", Depth: 1, IncludeMetadataLinks: true})
	if err != nil {
		t.Fatalf("query depth 1: %v", err)
	}
	if !hasNode(depth1, "tag:go") || !hasNode(depth1, "alpha") || !hasNode(depth1, "gamma") || hasNode(depth1, "beta") {
		t.Fatalf("depth 1 should keep hub and directly tagged notes only, got %#v", depth1.Nodes)
	}
	if hasEdgeKind(depth1, domain.GraphEdgeLinksTo) {
		t.Fatalf("depth 1 should not include note-link expansion edges, got %#v", depth1.Edges)
	}

	depth2, err := store.Query(ctx, domain.GraphQuery{MetadataSeed: "tag:go", Depth: 2, IncludeMetadataLinks: true})
	if err != nil {
		t.Fatalf("query depth 2: %v", err)
	}
	if !hasNode(depth2, "tag:go") || !hasNode(depth2, "alpha") || !hasNode(depth2, "beta") || !hasNode(depth2, "gamma") {
		t.Fatalf("depth 2 should include note-link neighbours from tagged notes, got %#v", depth2.Nodes)
	}
	if !hasEdgeKind(depth2, domain.GraphEdgeLinksTo) {
		t.Fatalf("depth 2 should include note-link expansion edges, got %#v", depth2.Edges)
	}
}

// With no seed and no predicates, Query behaves like a full-graph selection:
// every note (including isolated ones) appears.
func TestQueryFullGraphIncludesIsolatedNotes(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()
	if err := store.ReplaceAllLinks(ctx, []LinkProjection{
		{Source: "alpha"},
		{Source: "folder/beta"},
	}); err != nil {
		t.Fatalf("replace all links: %v", err)
	}
	g, err := store.Query(ctx, domain.GraphQuery{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !hasNode(g, "alpha") || !hasNode(g, "folder/beta") {
		t.Fatalf("expected isolated notes in full query, got %#v", g.Nodes)
	}
	// PathPrefix narrows to the folder.
	scoped, err := store.Query(ctx, domain.GraphQuery{PathPrefix: "folder/"})
	if err != nil {
		t.Fatalf("query prefix: %v", err)
	}
	if hasNode(scoped, "alpha") || !hasNode(scoped, "folder/beta") {
		t.Fatalf("expected path-scoped notes, got %#v", scoped.Nodes)
	}
}

// A negative depth means "no hop limit": the seed still focuses the view, but
// the selection covers every note, including ones the seed cannot reach at all.
// Metadata predicates still apply, since unbounded only removes the hop bound.
func TestQueryUnboundedDepthSelectsEveryNote(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
	ctx := context.Background()

	// A chain alpha -> beta -> gamma, plus an island with no path to the seed.
	beta := domain.NoteID("beta")
	gamma := domain.NoteID("gamma")
	if err := store.ReplaceOutgoingLinks(ctx, "alpha", []domain.NoteLink{{Target: "beta", ResolvedID: &beta, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOutgoingLinks(ctx, "beta", []domain.NoteLink{{Target: "gamma", ResolvedID: &gamma, Strength: domain.LinkStrengthHard}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alpha", "beta", "gamma", "island"} {
		if err := store.UpsertNoteMeta(ctx, NoteMeta{ID: domain.NoteID(id), Type: "concept"}); err != nil {
			t.Fatal(err)
		}
	}

	// Depth 1 stops at the seed's immediate neighbour.
	shallow, err := store.Query(ctx, domain.GraphQuery{Seed: idPtr("alpha"), Depth: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if hasNode(shallow, "gamma") || hasNode(shallow, "island") {
		t.Fatalf("depth 1 should stop at beta, got %#v", shallow.Nodes)
	}

	// Unbounded reaches the far end of the chain and the disconnected island.
	full, err := store.Query(ctx, domain.GraphQuery{Seed: idPtr("alpha"), Depth: domain.GraphDepthUnbounded})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, id := range []string{"alpha", "beta", "gamma", "island"} {
		if !hasNode(full, id) {
			t.Fatalf("unbounded depth should include %q, got %#v", id, full.Nodes)
		}
	}

	// The hop bound is the only thing dropped — predicates still narrow the set.
	filtered, err := store.Query(ctx, domain.GraphQuery{Seed: idPtr("alpha"), Depth: domain.GraphDepthUnbounded, Types: []string{"other"}})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(filtered.Nodes) != 0 {
		t.Fatalf("type predicate should still apply when unbounded, got %#v", filtered.Nodes)
	}
}
