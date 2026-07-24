package graph

import (
	"strings"

	"GoMental/internal/domain"
)

// Facet describes one kind of metadata-derived relation. Notes that share a facet
// value are connected through a single hub node (id = Prefix+value), so a value
// shared by N notes costs O(N) membership edges, not O(N^2) pairwise edges.
//
// The registry is the single extension point: add a Facet here (plus its domain
// strength/kind constants) to surface a new "metadata link" category.
type Facet struct {
	Strength domain.LinkStrength
	EdgeKind domain.GraphEdgeKind
	NodeKind domain.GraphNodeKind
	Prefix   string
	// Values extracts this note's facet values (already normalized, deduped,
	// non-empty). Membership hub keys are Prefix+value.
	Values func(domain.ParsedOKFNote) []string
}

// metadataFacets is the v1 registry: tag, type, heading.
var metadataFacets = []Facet{
	{
		Strength: domain.LinkStrengthTag,
		EdgeKind: domain.GraphEdgeTaggedWith,
		NodeKind: domain.GraphNodeTag,
		Prefix:   "tag:",
		Values: func(n domain.ParsedOKFNote) []string {
			out := make([]string, 0, len(n.Tags))
			seen := map[string]struct{}{}
			for _, t := range n.Tags {
				v := string(t)
				if v == "" {
					continue
				}
				if _, ok := seen[v]; ok {
					continue
				}
				seen[v] = struct{}{}
				out = append(out, v)
			}
			return out
		},
	},
	{
		Strength: domain.LinkStrengthType,
		EdgeKind: domain.GraphEdgeSharedType,
		NodeKind: domain.GraphNodeType,
		Prefix:   "type:",
		Values: func(n domain.ParsedOKFNote) []string {
			v := strings.TrimSpace(n.Metadata.Type)
			if v == "" {
				return nil
			}
			return []string{v}
		},
	},
	{
		Strength: domain.LinkStrengthHeading,
		EdgeKind: domain.GraphEdgeSharedHeading,
		NodeKind: domain.GraphNodeHeading,
		Prefix:   "heading:",
		Values: func(n domain.ParsedOKFNote) []string {
			var out []string
			seen := map[string]struct{}{}
			for _, h := range n.Headings {
				key := headingKey(h) // lowercased, matches sharedHeadings semantics
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, key)
			}
			return out
		},
	},
}

// MetadataMembership is one note→hub edge to be stored (strength + hub key).
type MetadataMembership struct {
	Strength domain.LinkStrength
	HubKey   string
}

// MetadataMemberships returns every facet-hub membership edge for a note.
func MetadataMemberships(note domain.ParsedOKFNote) []MetadataMembership {
	var out []MetadataMembership
	for _, f := range metadataFacets {
		for _, v := range f.Values(note) {
			out = append(out, MetadataMembership{Strength: f.Strength, HubKey: f.Prefix + v})
		}
	}
	return out
}

// edgeKindForStrength maps a metadata strength to its graph edge kind.
func edgeKindForStrength(s domain.LinkStrength) domain.GraphEdgeKind {
	for _, f := range metadataFacets {
		if f.Strength == s {
			return f.EdgeKind
		}
	}
	return domain.GraphEdgeInferredRelatedTo
}

// hubNodeKind classifies a hub node id by its facet prefix, returning the node
// kind and the label (value after the prefix).
func hubNodeKind(id string) (domain.GraphNodeKind, string, bool) {
	for _, f := range metadataFacets {
		if strings.HasPrefix(id, f.Prefix) {
			return f.NodeKind, strings.TrimPrefix(id, f.Prefix), true
		}
	}
	return "", "", false
}
