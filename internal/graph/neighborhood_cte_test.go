package graph

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"GoMental/internal/domain"
)

// neighborhoodOracle is a proper layered BFS reference: a node is included iff its
// minimum undirected hop-distance from id is <= depth, and an edge iff both its
// endpoints are. This is the intended depth-bounded semantics the recursive-CTE
// Neighborhood must match. (The pre-refactor implementation mutated its visited set
// mid-iteration, so a single round could chain several hops and over-include nodes
// beyond depth; the CTE fixes that.)
func neighborhoodOracle(t *testing.T, s *SQLiteStore, id domain.NoteID, depth int) domain.Graph {
	t.Helper()
	if depth <= 0 {
		depth = 1
	}
	edges, err := s.allEdges(context.Background(), domain.GraphFilter{IncludeHardLinks: true, IncludeSoftLinks: true, IncludeUnresolved: true})
	if err != nil {
		t.Fatalf("allEdges: %v", err)
	}
	dist := map[string]int{string(id): 0}
	frontier := []string{string(id)}
	for d := 0; d < depth; d++ {
		var next []string
		for _, node := range frontier {
			for _, edge := range edges {
				var other string
				switch {
				case edge.Source == node:
					other = edge.Target
				case edge.Target == node:
					other = edge.Source
				default:
					continue
				}
				if _, ok := dist[other]; !ok {
					dist[other] = d + 1
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	filtered := make([]domain.GraphEdge, 0)
	for _, edge := range edges {
		_, srcOK := dist[edge.Source]
		_, dstOK := dist[edge.Target]
		if srcOK && dstOK {
			filtered = append(filtered, edge)
		}
	}
	return graphFromEdges(filtered, true)
}

func nodeIDSet(g domain.Graph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)
	return ids
}

func edgeIDSet(g domain.Graph) []string {
	ids := make([]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestNeighborhoodCTEMatchesOracle(t *testing.T) {
	ctx := context.Background()
	for seed := int64(0); seed < 60; seed++ {
		rng := rand.New(rand.NewSource(seed))
		store := openStore(t, filepath.Join(t.TempDir(), "graph.sqlite"))
		n := 4 + rng.Intn(8)
		ids := make([]domain.NoteID, n)
		for i := range ids {
			ids[i] = domain.NoteID(fmt.Sprintf("n%d", i))
		}
		// Random hard links (some resolved, some dangling) + random soft links.
		for _, src := range ids {
			var hard []domain.NoteLink
			for k := 0; k < rng.Intn(3); k++ {
				if rng.Intn(4) == 0 {
					hard = append(hard, domain.NoteLink{Target: fmt.Sprintf("missing%d", k), Strength: domain.LinkStrengthHard})
				} else {
					tgt := ids[rng.Intn(n)]
					hard = append(hard, domain.NoteLink{Target: string(tgt), ResolvedID: &tgt, Strength: domain.LinkStrengthHard})
				}
			}
			if err := store.ReplaceOutgoingLinks(ctx, src, hard); err != nil {
				t.Fatal(err)
			}
			var soft []domain.InferredNoteLink
			for k := 0; k < rng.Intn(3); k++ {
				tgt := ids[rng.Intn(n)]
				if tgt == src {
					continue
				}
				soft = append(soft, domain.InferredNoteLink{Source: src, Target: tgt, Score: 0.5, Algorithm: "t"})
			}
			if err := store.ReplaceInferredLinks(ctx, src, soft); err != nil {
				t.Fatal(err)
			}
		}

		for _, depth := range []int{1, 2, 3} {
			start := ids[rng.Intn(n)]
			got, err := store.Neighborhood(ctx, start, depth)
			if err != nil {
				t.Fatalf("seed %d neighborhood: %v", seed, err)
			}
			want := neighborhoodOracle(t, store, start, depth)
			if !equalStrs(nodeIDSet(got), nodeIDSet(want)) {
				t.Fatalf("seed %d start %s depth %d: nodes\n got=%v\nwant=%v", seed, start, depth, nodeIDSet(got), nodeIDSet(want))
			}
			if !equalStrs(edgeIDSet(got), edgeIDSet(want)) {
				t.Fatalf("seed %d start %s depth %d: edges\n got=%v\nwant=%v", seed, start, depth, edgeIDSet(got), edgeIDSet(want))
			}
		}
		_ = store.Close()
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
