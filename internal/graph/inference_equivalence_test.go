package graph

import (
	"context"
	"reflect"
	"testing"
	"time"

	"GoMental/internal/domain"
)

// TestInferAllEqualsInferLinks is the load-bearing guard for the candidate-based
// refactor: InferAll must produce byte-identical results to the O(n^2) InferLinks
// for every note, across a corpus exercising overlapping tags, shared headings,
// title mentions, duplicate titles, substring titles, empty titles, and duplicate
// tag/heading multiplicity.
func TestInferAllEqualsInferLinks(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	service := NewLocalInferenceService(InferenceConfig{Now: func() time.Time { return now }})

	corpus := []domain.ParsedOKFNote{
		parsed("alpha", "Alpha", "Alpha mentions Beta and Gamma Systems here", []domain.Tag{"go", "db"}, []string{"Overview", "Design"}),
		parsed("beta", "Beta", "Beta talks about Alpha", []domain.Tag{"go"}, []string{"Overview"}),
		parsed("gamma", "Gamma Systems", "Gamma standalone body", []domain.Tag{"db", "db"}, []string{"Design", "Design"}),
		parsed("delta", "Delta", "no shared signal at all zzz", []domain.Tag{"lonely"}, []string{"Unique"}),
		parsed("epsilon", "Gamma Systems", "duplicate title with gamma, mentions Alpha", []domain.Tag{"go"}, []string{"Overview"}),
		parsed("zeta", "Ga", "substring title note; body says Alpha and Beta", []domain.Tag{"db"}, []string{"Design", "Overview"}),
		parsed("eta", "", "empty title note sharing tag go and heading Overview", []domain.Tag{"go"}, []string{"Overview"}),
		parsed("theta", "Theta", "Theta body mentions Delta and Alpha", []domain.Tag{"db", "go"}, []string{"Design"}),
	}

	idx := BuildCorpusIndex(corpus)
	all, err := service.InferAll(context.Background(), idx)
	if err != nil {
		t.Fatalf("InferAll: %v", err)
	}

	for _, note := range corpus {
		want, err := service.InferLinks(context.Background(), note, corpus)
		if err != nil {
			t.Fatalf("InferLinks(%s): %v", note.ID, err)
		}
		got := all[note.ID]
		// Normalize nil vs empty slice: InferLinks returns nil when no links.
		if len(want) == 0 && len(got) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mismatch for %s:\n got=%#v\nwant=%#v", note.ID, got, want)
		}
	}
}
