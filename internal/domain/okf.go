package domain

import "time"

type OKFMetadata struct {
	Type        string
	Title       string
	Description string
	Resource    string
	Tags        []Tag
	Timestamp   *time.Time
	Unknown     map[string]any
}

type ParsedOKFNote struct {
	ID         NoteID
	Document   OKFDocument
	Metadata   OKFMetadata
	Title      string
	Body       string
	PlainText  string
	Headings   []Heading
	Tags       []Tag
	Links      []ParsedLink
	ModifiedAt time.Time
}

type Heading struct {
	Level int
	Text  string
	Slug  string
}

type LinkKind string

const (
	LinkKindMarkdown LinkKind = "markdown"
	LinkKindWiki     LinkKind = "wiki"
)

type LinkStrength string

const (
	LinkStrengthHard LinkStrength = "hard"
	LinkStrengthSoft LinkStrength = "soft"
	// Metadata-family strengths: symmetric, deterministic membership edges from a
	// note to a facet-value hub node (see internal/graph/facets.go). They form the
	// "metadata links" the graph UI can filter on.
	LinkStrengthTag     LinkStrength = "tag"
	LinkStrengthType    LinkStrength = "type"
	LinkStrengthHeading LinkStrength = "heading"
)

// MetadataLinkStrengths lists the facet strengths gated by GraphFilter.IncludeMetadataLinks.
var MetadataLinkStrengths = []LinkStrength{LinkStrengthTag, LinkStrengthType, LinkStrengthHeading}

// IsMetadata reports whether s is a metadata-family (facet membership) strength.
func (s LinkStrength) IsMetadata() bool {
	switch s {
	case LinkStrengthTag, LinkStrengthType, LinkStrengthHeading:
		return true
	default:
		return false
	}
}

type ParsedLink struct {
	Source      NoteID
	RawTarget   string
	ResolvedID  *NoteID
	DisplayText string
	Heading     string
	Kind        LinkKind
	Strength    LinkStrength
}

type DecodeError struct {
	Code    string
	Message string
	Detail  string
}

func (e DecodeError) Error() string {
	if e.Detail == "" {
		return e.Message
	}
	return e.Message + ": " + e.Detail
}
