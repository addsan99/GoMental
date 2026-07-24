package graph

import (
	"context"
	"sort"
	"strings"
	"time"

	"GoMental/internal/domain"
)

const DefaultInferenceAlgorithm = "GoMental-local-v1"

type InferenceConfig struct {
	Threshold float64
	TopK      int
	Now       func() time.Time
}

type LocalInferenceService struct {
	config InferenceConfig
}

func NewLocalInferenceService(config InferenceConfig) LocalInferenceService {
	if config.Threshold <= 0 {
		config.Threshold = 0.3
	}
	if config.TopK <= 0 {
		config.TopK = 5
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return LocalInferenceService{config: config}
}

func (s LocalInferenceService) InferLinks(ctx context.Context, note domain.ParsedOKFNote, corpus []domain.ParsedOKFNote) ([]domain.InferredNoteLink, error) {
	var links []domain.InferredNoteLink
	for _, candidate := range corpus {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if candidate.ID == note.ID {
			continue
		}
		if link, ok := s.scoreCandidate(note, candidate); ok {
			links = append(links, link)
		}
	}
	return rankAndCap(links, s.config.TopK), nil
}

// InferAll computes the inferred (soft) links for every note in idx using
// candidate generation, returning results keyed by source note ID. It is exactly
// equivalent to calling InferLinks for each note against the full corpus (any note
// outside the candidate set scores 0), but runs in ~O(n*k) instead of O(n^2).
func (s LocalInferenceService) InferAll(ctx context.Context, idx *CorpusIndex) (map[domain.NoteID][]domain.InferredNoteLink, error) {
	result := make(map[domain.NoteID][]domain.InferredNoteLink)
	if idx == nil {
		return result, nil
	}
	if idx.dirty {
		idx.rebuildMatcher()
	}
	for _, id := range idx.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		src := idx.byID[id]
		if src == nil {
			continue
		}
		result[id] = s.inferForSource(idx, src)
	}
	return result, nil
}

// InferOne computes the soft links for a single source note using the index.
func (s LocalInferenceService) InferOne(ctx context.Context, idx *CorpusIndex, id domain.NoteID) ([]domain.InferredNoteLink, error) {
	if idx == nil {
		return nil, nil
	}
	if idx.dirty {
		idx.rebuildMatcher()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	src := idx.byID[id]
	if src == nil {
		return nil, nil
	}
	return s.inferForSource(idx, src), nil
}

// inferForSource scores all candidates for one source and ranks/caps them. Shared
// by InferAll and InferOne so both stay identical to the O(n^2) InferLinks.
func (s LocalInferenceService) inferForSource(idx *CorpusIndex, src *noteMeta) []domain.InferredNoteLink {
	source := src.toParsed()
	cands := idx.candidatesFor(src)
	links := make([]domain.InferredNoteLink, 0, len(cands))
	for candID := range cands {
		cand := idx.byID[candID]
		if cand == nil {
			continue
		}
		if link, ok := s.scoreCandidate(source, cand.toParsed()); ok {
			links = append(links, link)
		}
	}
	return rankAndCap(links, s.config.TopK)
}

// scoreCandidate scores a single (source, candidate) pair with the shared
// evidence rules and threshold. It is the one place InferLinks and InferAll agree,
// so they cannot drift.
func (s LocalInferenceService) scoreCandidate(source, candidate domain.ParsedOKFNote) (domain.InferredNoteLink, bool) {
	evidence := inferEvidence(source, candidate)
	score := 0.0
	for _, item := range evidence {
		score += item.Weight
	}
	if score < s.config.Threshold {
		return domain.InferredNoteLink{}, false
	}
	return domain.InferredNoteLink{
		Source:     source.ID,
		Target:     candidate.ID,
		Score:      score,
		Evidence:   evidence,
		Algorithm:  DefaultInferenceAlgorithm,
		ComputedAt: s.config.Now().UTC(),
	}, true
}

// rankAndCap sorts by score descending (ties broken by target ID) and truncates
// to topK, matching the original InferLinks ordering.
func rankAndCap(links []domain.InferredNoteLink, topK int) []domain.InferredNoteLink {
	sort.Slice(links, func(i, j int) bool {
		if links[i].Score == links[j].Score {
			return links[i].Target < links[j].Target
		}
		return links[i].Score > links[j].Score
	})
	if len(links) > topK {
		links = links[:topK]
	}
	return links
}

func (s LocalInferenceService) Explain(ctx context.Context, source domain.NoteID, target domain.NoteID) (domain.LinkEvidence, error) {
	if err := ctx.Err(); err != nil {
		return domain.LinkEvidence{}, err
	}
	return domain.LinkEvidence{Kind: "explanation", Detail: string(source) + " -> " + string(target), Weight: 0}, nil
}

// inferEvidence scores a soft (inferred) relation. As of the metadata-links work,
// soft inference is PURELY text/semantic: the sole signal is target's title
// appearing in source's text. Faceted equality (same tag/type/heading) is now
// modeled as metadata hub links (see facets.go), not soft evidence.
func inferEvidence(source, target domain.ParsedOKFNote) []domain.LinkEvidence {
	var evidence []domain.LinkEvidence
	body := strings.ToLower(source.PlainText + " " + source.Body)
	if title := strings.ToLower(strings.TrimSpace(target.Title)); title != "" && strings.Contains(body, title) {
		evidence = append(evidence, domain.LinkEvidence{Kind: domain.EvidenceTitleMention, Detail: target.Title, Weight: 0.7})
	}
	return evidence
}
