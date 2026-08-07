package graph

import (
	"testing"

	"GoMental/internal/domain"
)

func TestSuggestLinksRanksMentionsAndExcludesExistingLinks(t *testing.T) {
	betaID := domain.NoteID("beta")
	corpus := []domain.ParsedOKFNote{
		{ID: "alpha", Title: "Alpha"},
		{ID: betaID, Title: "Beta note", Tags: []domain.Tag{"go"}, Metadata: domain.OKFMetadata{Type: "term", Tags: []domain.Tag{"go"}}},
		{ID: "gamma", Title: "Gamma note", Tags: []domain.Tag{"go"}, Metadata: domain.OKFMetadata{Type: "term", Tags: []domain.Tag{"go"}}},
	}
	idx := BuildCorpusIndex(corpus)
	source := domain.ParsedOKFNote{
		ID: "alpha", Title: "Alpha", PlainText: "Beta note is central to this design.",
		Tags: []domain.Tag{"go"}, Metadata: domain.OKFMetadata{Type: "term", Tags: []domain.Tag{"go"}},
	}
	got := idx.SuggestLinks(source, map[domain.NoteID]float64{betaID: 10, "gamma": 5}, SuggestionConfig{Threshold: 0.2})
	if len(got) != 2 || got[0].Target != betaID || !got[0].Mentioned {
		t.Fatalf("unexpected suggestions: %#v", got)
	}

	source.Links = []domain.ParsedLink{{ResolvedID: &betaID}}
	got = idx.SuggestLinks(source, map[domain.NoteID]float64{betaID: 10, "gamma": 5}, SuggestionConfig{Threshold: 0.2})
	if len(got) != 1 || got[0].Target != "gamma" {
		t.Fatalf("expected existing link to be excluded, got %#v", got)
	}
}

func TestSuggestLinksExcludesSelfReservedAndMetadataOnlyAtDefaultThreshold(t *testing.T) {
	idx := BuildCorpusIndex([]domain.ParsedOKFNote{
		{ID: "alpha", Title: "Alpha", Tags: []domain.Tag{"common"}, Metadata: domain.OKFMetadata{Type: "term"}},
		{ID: "index", Title: "Workspace Index"},
		{ID: "other", Title: "Other", Tags: []domain.Tag{"common"}, Metadata: domain.OKFMetadata{Type: "term"}},
	})
	source := domain.ParsedOKFNote{ID: "alpha", Title: "Alpha", Tags: []domain.Tag{"common"}, Metadata: domain.OKFMetadata{Type: "term"}}
	got := idx.SuggestLinks(source, map[domain.NoteID]float64{"alpha": 1, "index": 1, "other": 0}, SuggestionConfig{})
	if len(got) != 0 {
		t.Fatalf("expected no default-threshold suggestions, got %#v", got)
	}
}
