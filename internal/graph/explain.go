package graph

import (
	"strings"

	"GoMental/internal/domain"
)

// facetEvidence maps a metadata-facet strength to the evidence it contributes
// when two notes share that facet value. Weights mirror the facet hierarchy
// (tag > type > heading) and stay below the title-mention weight (0.7).
var facetEvidence = map[domain.LinkStrength]struct {
	Kind   domain.EvidenceKind
	Weight float64
	Prefix string
}{
	domain.LinkStrengthTag:     {domain.EvidenceSharedTag, 0.25, "tag:"},
	domain.LinkStrengthType:    {domain.EvidenceSharedType, 0.20, "type:"},
	domain.LinkStrengthHeading: {domain.EvidenceSharedHeading, 0.15, "heading:"},
}

// ExplainRelation computes the content-derived evidence that source and target
// are related: title mentions in either direction (reusing inferEvidence) plus
// shared tag/type/heading facets (reusing MetadataMemberships). It does not
// consult the graph store, so HardLink is always false here — the application
// layer sets it from the persisted hard links. Score is the summed evidence
// weight, clamped to 1.0; Related is true when any evidence was found.
func ExplainRelation(source, target domain.ParsedOKFNote) domain.LinkExplanation {
	expl := domain.LinkExplanation{Source: source.ID, Target: target.ID}

	// Title mentions are directed: inferEvidence(a, b) fires when b's title
	// appears in a's text. Annotate each with the direction for the reader.
	for _, ev := range inferEvidence(source, target) {
		if ev.Kind == domain.EvidenceTitleMention {
			ev.Detail = string(source.ID) + " mentions \"" + ev.Detail + "\""
		}
		expl.Evidence = append(expl.Evidence, ev)
	}
	for _, ev := range inferEvidence(target, source) {
		if ev.Kind == domain.EvidenceTitleMention {
			ev.Detail = string(target.ID) + " mentions \"" + ev.Detail + "\""
		}
		expl.Evidence = append(expl.Evidence, ev)
	}

	// Shared metadata facets: intersect the two notes' membership hub keys.
	targetHubs := make(map[string]struct{})
	for _, m := range MetadataMemberships(target) {
		targetHubs[m.HubKey] = struct{}{}
	}
	for _, m := range MetadataMemberships(source) {
		if _, ok := targetHubs[m.HubKey]; !ok {
			continue
		}
		fe, ok := facetEvidence[m.Strength]
		if !ok {
			continue
		}
		expl.Evidence = append(expl.Evidence, domain.LinkEvidence{
			Kind:   fe.Kind,
			Detail: strings.TrimPrefix(m.HubKey, fe.Prefix),
			Weight: fe.Weight,
		})
	}

	for _, ev := range expl.Evidence {
		expl.Score += ev.Weight
	}
	if expl.Score > 1.0 {
		expl.Score = 1.0
	}
	expl.Related = len(expl.Evidence) > 0
	return expl
}
