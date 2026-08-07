package graph

import (
	"sort"
	"strings"

	"GoMental/internal/domain"
)

const SuggestedLinksAlgorithm = "GoMental-local-v2"

type SuggestionConfig struct {
	Threshold float64
	Limit     int
}

type LinkSuggestion struct {
	Target    domain.NoteID
	Title     string
	Score     float64
	Evidence  []domain.LinkEvidence
	Mentioned bool
}

// SuggestLinks ranks a bounded set of lexical search results plus every exact
// title mention in source. lexical contains backend search scores and is
// normalized within the request before being combined with local corpus facts.
func (idx *CorpusIndex) SuggestLinks(source domain.ParsedOKFNote, lexical map[domain.NoteID]float64, config SuggestionConfig) []LinkSuggestion {
	if idx == nil {
		return nil
	}
	if config.Threshold <= 0 {
		config.Threshold = 0.45
	}
	if config.Limit <= 0 || config.Limit > 10 {
		config.Limit = 5
	}
	if idx.dirty {
		idx.rebuildMatcher()
	}

	candidates := make(map[domain.NoteID]struct{}, len(lexical))
	maxLexical := 0.0
	for id, score := range lexical {
		candidates[id] = struct{}{}
		if score > maxLexical {
			maxLexical = score
		}
	}
	text := strings.ToLower(source.PlainText + " " + source.Body)
	mentioned := map[domain.NoteID]struct{}{}
	if idx.matcher != nil {
		for id := range idx.matcher.FindNoteIDs(text) {
			candidates[id] = struct{}{}
			mentioned[id] = struct{}{}
		}
	}

	existing := resolvedTargetSet(source.Links)
	var out []LinkSuggestion
	for id := range candidates {
		if id == source.ID || reservedSuggestionTarget(id) {
			continue
		}
		if _, linked := existing[id]; linked {
			continue
		}
		target := idx.byID[id]
		if target == nil {
			continue
		}
		_, hasMention := mentioned[id]
		lexicalScore := 0.0
		if maxLexical > 0 {
			lexicalScore = lexical[id] / maxLexical
			if lexicalScore > 1 {
				lexicalScore = 1
			}
		}
		metadataScore, metadataEvidence := suggestionMetadata(source, target)
		graphScore, graphEvidence := suggestionGraph(source, target)
		mentionScore := 0.0
		var evidence []domain.LinkEvidence
		if hasMention {
			mentionScore = 1
			evidence = append(evidence, domain.LinkEvidence{Kind: domain.EvidenceTitleMention, Detail: target.Title, Weight: 0.30})
		}
		if lexicalScore > 0.05 {
			evidence = append(evidence, domain.LinkEvidence{Kind: domain.EvidenceLexicalMatch, Detail: "similar content", Weight: 0.45 * lexicalScore})
		}
		evidence = append(evidence, metadataEvidence...)
		evidence = append(evidence, graphEvidence...)
		score := 0.45*lexicalScore + 0.30*mentionScore + 0.15*metadataScore + 0.10*graphScore
		if hasMention && titleAmbiguous(target.Title) {
			score -= 0.15
		}
		if score < config.Threshold {
			continue
		}
		if score > 1 {
			score = 1
		}
		out = append(out, LinkSuggestion{Target: id, Title: target.Title, Score: score, Evidence: evidence, Mentioned: hasMention})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Target < out[j].Target
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > config.Limit {
		out = out[:config.Limit]
	}
	return out
}

func suggestionMetadata(source domain.ParsedOKFNote, target *noteMeta) (float64, []domain.LinkEvidence) {
	sharedTags := intersectTags(source.Tags, target.Tags)
	score := 0.0
	var evidence []domain.LinkEvidence
	if len(sharedTags) > 0 {
		union := len(source.Tags) + len(target.Tags) - len(sharedTags)
		if union > 0 {
			score += 0.75 * float64(len(sharedTags)) / float64(union)
		}
		for _, tag := range sharedTags {
			evidence = append(evidence, domain.LinkEvidence{Kind: domain.EvidenceSharedTag, Detail: string(tag), Weight: 0.15 * score})
		}
	}
	if source.Metadata.Type != "" && source.Metadata.Type == target.Type {
		score += 0.15
		evidence = append(evidence, domain.LinkEvidence{Kind: domain.EvidenceSharedType, Detail: target.Type, Weight: 0.0225})
	}
	sharedHeadings := intersectHeadings(source.Headings, target.Headings)
	if len(sharedHeadings) > 0 {
		score += 0.10
		evidence = append(evidence, domain.LinkEvidence{Kind: domain.EvidenceSharedHeading, Detail: sharedHeadings[0], Weight: 0.015})
	}
	if score > 1 {
		score = 1
	}
	return score, evidence
}

func suggestionGraph(source domain.ParsedOKFNote, target *noteMeta) (float64, []domain.LinkEvidence) {
	sourceTargets := resolvedTargetSet(source.Links)
	if len(sourceTargets) == 0 || len(target.LinkTargets) == 0 {
		return 0, nil
	}
	shared := 0
	detail := ""
	for _, id := range target.LinkTargets {
		if _, ok := sourceTargets[id]; ok {
			shared++
			if detail == "" {
				detail = string(id)
			}
		}
	}
	if shared == 0 {
		return 0, nil
	}
	union := len(sourceTargets) + len(target.LinkTargets) - shared
	score := float64(shared) / float64(union)
	return score, []domain.LinkEvidence{{Kind: domain.EvidenceSharedLink, Detail: detail, Weight: 0.10 * score}}
}

func resolvedTargetSet(links []domain.ParsedLink) map[domain.NoteID]struct{} {
	out := make(map[domain.NoteID]struct{}, len(links))
	for _, link := range links {
		if link.ResolvedID != nil {
			out[*link.ResolvedID] = struct{}{}
		}
	}
	return out
}

func intersectTags(a, b []domain.Tag) []domain.Tag {
	set := make(map[domain.Tag]struct{}, len(a))
	for _, item := range a {
		set[item] = struct{}{}
	}
	var out []domain.Tag
	for _, item := range b {
		if _, ok := set[item]; ok {
			out = append(out, item)
		}
	}
	return out
}

func intersectHeadings(a, b []domain.Heading) []string {
	set := make(map[string]struct{}, len(a))
	for _, item := range a {
		set[headingKey(item)] = struct{}{}
	}
	var out []string
	for _, item := range b {
		if _, ok := set[headingKey(item)]; ok {
			out = append(out, item.Text)
		}
	}
	return out
}

func reservedSuggestionTarget(id domain.NoteID) bool {
	parts := strings.Split(strings.ToLower(string(id)), "/")
	last := parts[len(parts)-1]
	return last == "index" || last == "log"
}

func titleAmbiguous(title string) bool {
	trimmed := strings.TrimSpace(title)
	return len([]rune(trimmed)) < 4 || len(strings.Fields(trimmed)) == 1 && len([]rune(trimmed)) < 7
}
