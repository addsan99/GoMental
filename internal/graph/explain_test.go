package graph

import (
	"testing"

	"GoMental/internal/domain"
)

func explainNote(id, title string, tags []string, body string) domain.ParsedOKFNote {
	tt := make([]domain.Tag, len(tags))
	for i, tag := range tags {
		tt[i] = domain.Tag(tag)
	}
	return domain.ParsedOKFNote{
		ID:        domain.NoteID(id),
		Title:     title,
		Tags:      tt,
		Body:      body,
		PlainText: body,
	}
}

func hasEvidence(evs []domain.LinkEvidence, kind domain.EvidenceKind) bool {
	for _, e := range evs {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func TestExplainRelationTitleMentionAndSharedTag(t *testing.T) {
	alpha := explainNote("alpha", "Alpha", []string{"go"}, "See Beta for details.")
	beta := explainNote("beta", "Beta", []string{"go"}, "Standalone note.")

	expl := ExplainRelation(alpha, beta)
	if !expl.Related {
		t.Fatal("expected related")
	}
	if !hasEvidence(expl.Evidence, domain.EvidenceTitleMention) {
		t.Fatalf("expected title_mention evidence: %+v", expl.Evidence)
	}
	if !hasEvidence(expl.Evidence, domain.EvidenceSharedTag) {
		t.Fatalf("expected shared_tag evidence: %+v", expl.Evidence)
	}
	if expl.Score <= 0 {
		t.Fatalf("expected positive score, got %v", expl.Score)
	}
	if expl.HardLink {
		t.Fatal("ExplainRelation must not set HardLink")
	}
}

func TestExplainRelationUnrelated(t *testing.T) {
	a := explainNote("a", "Apple", []string{"fruit"}, "Nothing here.")
	b := explainNote("b", "Zebra", []string{"animal"}, "Different content.")

	expl := ExplainRelation(a, b)
	if expl.Related || len(expl.Evidence) != 0 {
		t.Fatalf("expected unrelated, got %+v", expl)
	}
	if expl.Score != 0 {
		t.Fatalf("expected zero score, got %v", expl.Score)
	}
}
