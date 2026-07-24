package graph

import (
	"context"
	"testing"

	"GoMental/internal/domain"
)

func metaNote(id domain.NoteID, typ string, tags []domain.Tag, headings []string) domain.ParsedOKFNote {
	hs := make([]domain.Heading, len(headings))
	for i, h := range headings {
		hs[i] = domain.Heading{Level: 1, Text: h}
	}
	return domain.ParsedOKFNote{ID: id, Metadata: domain.OKFMetadata{Type: typ}, Tags: tags, Headings: hs}
}

func edgeSet(g domain.Graph) map[string]domain.GraphEdgeKind {
	out := map[string]domain.GraphEdgeKind{}
	for _, e := range g.Edges {
		out[e.Source+"->"+e.Target] = e.Kind
	}
	return out
}

func nodeKinds(g domain.Graph) map[string]domain.GraphNodeKind {
	out := map[string]domain.GraphNodeKind{}
	for _, n := range g.Nodes {
		out[n.ID] = n.Kind
	}
	return out
}

func TestMetadataLinksFacetsAndFilterGating(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, GraphPath(t.TempDir()))

	notes := []domain.ParsedOKFNote{
		metaNote("alpha", "concept", []domain.Tag{"go"}, []string{"Overview"}),
		metaNote("beta", "concept", []domain.Tag{"go", "db"}, []string{"Design"}),
	}
	for _, n := range notes {
		if err := store.ReplaceMetadataLinks(ctx, n.ID, MetadataMemberships(n)); err != nil {
			t.Fatalf("replace metadata %s: %v", n.ID, err)
		}
	}

	// Without the filter, metadata edges are invisible.
	plain, err := store.FullGraph(ctx, domain.GraphFilter{})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if len(plain.Edges) != 0 {
		t.Fatalf("expected no edges without IncludeMetadataLinks, got %#v", plain.Edges)
	}

	g, err := store.FullGraph(ctx, domain.GraphFilter{IncludeMetadataLinks: true})
	if err != nil {
		t.Fatalf("full graph metadata: %v", err)
	}
	edges := edgeSet(g)
	want := map[string]domain.GraphEdgeKind{
		"alpha->tag:go":           domain.GraphEdgeTaggedWith,
		"beta->tag:go":            domain.GraphEdgeTaggedWith,
		"beta->tag:db":            domain.GraphEdgeTaggedWith,
		"alpha->type:concept":     domain.GraphEdgeSharedType,
		"beta->type:concept":      domain.GraphEdgeSharedType,
		"alpha->heading:overview": domain.GraphEdgeSharedHeading,
		"beta->heading:design":    domain.GraphEdgeSharedHeading,
	}
	if len(edges) != len(want) {
		t.Fatalf("edge count = %d, want %d\n got=%#v", len(edges), len(want), edges)
	}
	for k, kind := range want {
		if edges[k] != kind {
			t.Fatalf("edge %q kind = %q, want %q", k, edges[k], kind)
		}
	}

	kinds := nodeKinds(g)
	if kinds["tag:go"] != domain.GraphNodeTag || kinds["type:concept"] != domain.GraphNodeType || kinds["heading:overview"] != domain.GraphNodeHeading {
		t.Fatalf("hub node kinds wrong: %#v", kinds)
	}
	if kinds["alpha"] != domain.GraphNodeNote {
		t.Fatalf("note node kind wrong: %#v", kinds)
	}
	// Hub label is the value after the prefix.
	for _, n := range g.Nodes {
		if n.ID == "tag:go" && n.Label != "go" {
			t.Fatalf("hub label = %q, want go", n.Label)
		}
	}
}

func TestMetadataLinksReplaceAndDelete(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, GraphPath(t.TempDir()))

	alpha := metaNote("alpha", "concept", []domain.Tag{"go"}, nil)
	beta := metaNote("beta", "concept", []domain.Tag{"go"}, nil)
	for _, n := range []domain.ParsedOKFNote{alpha, beta} {
		if err := store.ReplaceMetadataLinks(ctx, n.ID, MetadataMemberships(n)); err != nil {
			t.Fatalf("replace: %v", err)
		}
	}

	// Re-membership alpha with a different tag; the old tag edge must be gone.
	if err := store.ReplaceMetadataLinks(ctx, "alpha", MetadataMemberships(metaNote("alpha", "concept", []domain.Tag{"rust"}, nil))); err != nil {
		t.Fatalf("replace alpha: %v", err)
	}
	g, _ := store.FullGraph(ctx, domain.GraphFilter{IncludeMetadataLinks: true})
	edges := edgeSet(g)
	if _, ok := edges["alpha->tag:go"]; ok {
		t.Fatalf("stale alpha->tag:go edge remains")
	}
	if _, ok := edges["alpha->tag:rust"]; !ok {
		t.Fatalf("new alpha->tag:rust edge missing")
	}
	// The tag:go hub still exists via beta.
	if _, ok := nodeKinds(g)["tag:go"]; !ok {
		t.Fatalf("tag:go hub should persist via beta")
	}

	// Deleting beta removes its last edge to tag:go, so the hub disappears.
	if err := store.DeleteNote(ctx, "beta"); err != nil {
		t.Fatalf("delete beta: %v", err)
	}
	g, _ = store.FullGraph(ctx, domain.GraphFilter{IncludeMetadataLinks: true})
	if _, ok := nodeKinds(g)["tag:go"]; ok {
		t.Fatalf("tag:go hub should be gone after last member deleted")
	}
}

func TestMetadataMembershipsEmptyNote(t *testing.T) {
	if m := MetadataMemberships(domain.ParsedOKFNote{ID: "empty"}); len(m) != 0 {
		t.Fatalf("expected no memberships for empty note, got %#v", m)
	}
}
