package application

import (
	"sort"
	"testing"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
)

func sortedIDs(ids []domain.NoteID) []domain.NoteID {
	out := append([]domain.NoteID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func equalIDs(a, b []domain.NoteID) bool {
	a, b = sortedIDs(a), sortedIDs(b)
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

func TestLiveCorpusResolverIDsMatchRebuild(t *testing.T) {
	base := []domain.ParsedOKFNote{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	lc := newLiveCorpus(graph.BuildCorpusIndex(base))

	lc.Upsert(domain.ParsedOKFNote{ID: "d"}) // new
	lc.Upsert(domain.ParsedOKFNote{ID: "b"}) // existing, must not duplicate
	lc.Delete("a")                            // remove
	lc.Delete("zzz")                          // no-op

	want := []domain.NoteID{"b", "c", "d"}
	if got := lc.ResolverIDs(); !equalIDs(got, want) {
		t.Fatalf("ResolverIDs = %v, want %v", sortedIDs(got), want)
	}

	// Must equal a from-scratch index over the same effective note set.
	rebuilt := graph.BuildCorpusIndex([]domain.ParsedOKFNote{{ID: "b"}, {ID: "c"}, {ID: "d"}})
	if !equalIDs(lc.ResolverIDs(), rebuilt.ResolverIDs()) {
		t.Fatalf("incremental IDs %v != rebuilt %v", sortedIDs(lc.ResolverIDs()), sortedIDs(rebuilt.ResolverIDs()))
	}
}

func TestLiveCorpusNilSafe(t *testing.T) {
	var lc *liveCorpus = newLiveCorpus(nil)
	lc.Upsert(domain.ParsedOKFNote{ID: "a"})
	lc.Delete("a")
	if ids := lc.ResolverIDs(); ids != nil {
		t.Fatalf("expected nil IDs from nil index, got %v", ids)
	}
	if snap := lc.Snapshot(); snap != nil {
		t.Fatalf("expected nil snapshot from nil index")
	}
}
