package graph

import (
	"context"
	"testing"
	"time"

	"GoMental/internal/domain"
)

func TestLocalInferenceServiceScoresThresholdsAndLimits(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	service := NewLocalInferenceService(InferenceConfig{Threshold: 0.3, TopK: 1, Now: func() time.Time { return now }})
	source := parsed("alpha", "Alpha", "This note mentions Beta Systems", []domain.Tag{"go"}, []string{"Shared"})
	corpus := []domain.ParsedOKFNote{
		source,
		parsed("beta", "Beta Systems", "body", []domain.Tag{"go"}, []string{"Shared"}),
		parsed("gamma", "Gamma", "body", []domain.Tag{"other"}, []string{"Nope"}),
	}
	links, err := service.InferLinks(context.Background(), source, corpus)
	if err != nil {
		t.Fatalf("infer links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected top 1 link, got %#v", links)
	}
	link := links[0]
	// Soft inference is now title-only: the single signal is target's title
	// appearing in source's text (weight 0.7). Tag/heading equality is modeled as
	// metadata hub links, not soft evidence.
	if link.Target != "beta" || link.Score != 0.7 || link.Algorithm != DefaultInferenceAlgorithm || !link.ComputedAt.Equal(now) {
		t.Fatalf("unexpected inferred link: %#v", link)
	}
	if len(link.Evidence) != 1 || link.Evidence[0].Kind != domain.EvidenceTitleMention {
		t.Fatalf("expected single title-mention evidence, got %#v", link.Evidence)
	}
}

func parsed(id domain.NoteID, title string, body string, tags []domain.Tag, headings []string) domain.ParsedOKFNote {
	hs := make([]domain.Heading, len(headings))
	for i, heading := range headings {
		hs[i] = domain.Heading{Level: 1, Text: heading}
	}
	return domain.ParsedOKFNote{ID: id, Title: title, Body: body, PlainText: body, Tags: tags, Headings: hs}
}
