package domain

import (
	"context"
	"time"
)

type NoteLink struct {
	Source      NoteID
	Target      string
	ResolvedID  *NoteID
	DisplayText string
	Heading     string
	Strength    LinkStrength
}

type EvidenceKind string

const (
	EvidenceTitleMention  EvidenceKind = "title_mention"
	EvidenceSharedTag     EvidenceKind = "shared_tag"
	EvidenceSharedType    EvidenceKind = "shared_type"
	EvidenceSharedHeading EvidenceKind = "shared_heading"
	EvidenceLexicalMatch  EvidenceKind = "lexical_similarity"
	EvidenceSharedLink    EvidenceKind = "shared_link"
)

type LinkEvidence struct {
	Kind   EvidenceKind
	Detail string
	Weight float64
}

// LinkExplanation is the "why are these two notes related?" answer surfaced by
// the explain_link tool: an explicit hard-link flag plus the content-derived
// evidence (title mentions and shared tag/type/heading facets) and a combined
// score. Related is true when there is a hard link or any evidence.
type LinkExplanation struct {
	Source   NoteID
	Target   NoteID
	Related  bool
	HardLink bool
	Score    float64
	Evidence []LinkEvidence
}

type InferredNoteLink struct {
	Source     NoteID
	Target     NoteID
	Score      float64
	Evidence   []LinkEvidence
	Algorithm  string
	ComputedAt time.Time
}

type GraphNodeKind string

const (
	GraphNodeNote       GraphNodeKind = "note"
	GraphNodeUnresolved GraphNodeKind = "unresolved"
	GraphNodeTag        GraphNodeKind = "tag"
	GraphNodeType       GraphNodeKind = "type"
	GraphNodeHeading    GraphNodeKind = "heading"
)

type GraphEdgeKind string

const (
	GraphEdgeLinksTo           GraphEdgeKind = "links_to"
	GraphEdgeInferredRelatedTo GraphEdgeKind = "inferred_related_to"
	GraphEdgeTaggedWith        GraphEdgeKind = "tagged_with"
	GraphEdgeSharedType        GraphEdgeKind = "shared_type"
	GraphEdgeSharedHeading     GraphEdgeKind = "shared_heading"
)

type GraphNode struct {
	ID         string
	Label      string
	Kind       GraphNodeKind
	NoteID     *NoteID
	Tags       []Tag
	ModifiedAt *time.Time
}

type GraphEdge struct {
	ID     string
	Source string
	Target string
	Kind   GraphEdgeKind
	Score  float64
}

type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

type GraphFilter struct {
	Tags                 []Tag
	PathPrefix           string
	FavoritesOnly        bool
	IncludeUnresolved    bool
	IncludeOrphans       bool
	IncludeSoftLinks     bool
	IncludeHardLinks     bool
	IncludeMetadataLinks bool
	Depth                int
}

// GraphQuery is the unified selection request that generalizes Neighborhood and
// FullGraph. When Seed is nil the query is a full-graph selection; when Seed is
// set it is a depth-bounded neighborhood around that note. In both cases the
// metadata predicates (Types, Tags, PathPrefix) restrict the note node set:
// predicates are AND across axes, OR within an axis, and an empty axis imposes no
// restriction. The predicates apply only to note nodes — metadata hub nodes
// (tag/type/heading) and unresolved nodes are not filtered by them.
type GraphQuery struct {
	Seed         *NoteID // optional focus note; nil = full graph
	MetadataSeed string  // optional metadata hub focus (e.g. "tag:go")
	Depth        int     // hops from Seed/MetadataSeed; ignored when both are empty

	// Metadata predicates restricting which note nodes are included.
	Types         []string
	Tags          []Tag
	PathPrefix    string
	FavoritesOnly bool

	IncludeSoftLinks     bool
	IncludeMetadataLinks bool
	IncludeUnresolved    bool
}

type GraphStore interface {
	ReplaceOutgoingLinks(ctx context.Context, source NoteID, links []NoteLink) error
	DeleteNote(ctx context.Context, id NoteID) error
	OutgoingLinks(ctx context.Context, id NoteID) ([]NoteLink, error)
	Backlinks(ctx context.Context, id NoteID) ([]NoteLink, error)
	Neighborhood(ctx context.Context, id NoteID, depth int) (Graph, error)
	FullGraph(ctx context.Context, filter GraphFilter) (Graph, error)
	Query(ctx context.Context, query GraphQuery) (Graph, error)
}

type LinkInferenceService interface {
	InferLinks(ctx context.Context, note ParsedOKFNote, corpus []ParsedOKFNote) ([]InferredNoteLink, error)
	Explain(ctx context.Context, source NoteID, target NoteID) (LinkEvidence, error)
}
