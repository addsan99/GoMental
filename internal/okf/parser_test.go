package okf

import (
	"errors"
	"testing"
	"time"

	"GoMental/internal/domain"
)

func TestParseNoteExtractsOKFMetadataHeadingsTagsAndLinks(t *testing.T) {
	raw := `---
type: concept
title: Frontmatter Title
description: A note
resource: https://example.test
unknown_field: keep me
tags:
  - Go
  - "#OKF"
timestamp: 2026-07-14T00:00:00Z
---

# Body Title

See [Beta](../beta.md#Details), [Root](/root/topic.md), and [[Gamma Note|Gamma alias]].
![Image](image.png)
`
	parsed, err := NewParser().ParseNote("folder/alpha", raw, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if parsed.Metadata.Type != "concept" {
		t.Fatalf("expected type concept, got %q", parsed.Metadata.Type)
	}
	if parsed.Title != "Frontmatter Title" {
		t.Fatalf("expected frontmatter title, got %q", parsed.Title)
	}
	if len(parsed.Tags) != 2 || parsed.Tags[0] != "go" || parsed.Tags[1] != "okf" {
		t.Fatalf("unexpected tags: %#v", parsed.Tags)
	}
	if parsed.Metadata.Timestamp == nil || parsed.Metadata.Timestamp.UTC().Format(time.RFC3339) != "2026-07-14T00:00:00Z" {
		t.Fatalf("expected timestamp, got %#v", parsed.Metadata.Timestamp)
	}
	if parsed.Metadata.Unknown["unknown_field"] != "keep me" {
		t.Fatalf("unknown frontmatter not preserved: %#v", parsed.Metadata.Unknown)
	}
	if len(parsed.Headings) != 1 || parsed.Headings[0].Text != "Body Title" || parsed.Headings[0].Slug != "body-title" {
		t.Fatalf("unexpected headings: %#v", parsed.Headings)
	}
	if len(parsed.Links) != 3 {
		t.Fatalf("expected three non-image links, got %#v", parsed.Links)
	}
	assertLink(t, parsed.Links[0], domain.LinkKindMarkdown, "../beta.md", "Beta", "Details")
	assertLink(t, parsed.Links[1], domain.LinkKindMarkdown, "/root/topic.md", "Root", "")
	assertLink(t, parsed.Links[2], domain.LinkKindWiki, "Gamma Note", "Gamma alias", "")
	if parsed.PlainText == "" || parsed.Body == "" {
		t.Fatal("expected body and plain text")
	}
}

func TestParseNoteTitleFallback(t *testing.T) {
	parsed, err := NewParser().ParseNote("folder/fallback", "---\ntype: concept\n---\n\n## First Heading\n", time.Time{})
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if parsed.Title != "First Heading" {
		t.Fatalf("expected heading title fallback, got %q", parsed.Title)
	}

	parsed, err = NewParser().ParseNote("folder/basename", "---\ntype: concept\n---\n\nNo headings\n", time.Time{})
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if parsed.Title != "basename" {
		t.Fatalf("expected basename fallback, got %q", parsed.Title)
	}
}

func TestParseNoteReturnsStructuredErrors(t *testing.T) {
	_, err := NewParser().ParseNote("bad", "No frontmatter", time.Time{})
	var decodeErr domain.DecodeError
	if !errors.As(err, &decodeErr) || decodeErr.Code != "okf.missing_frontmatter" {
		t.Fatalf("expected missing frontmatter decode error, got %v", err)
	}

	_, err = NewParser().ParseNote("bad", "---\ntitle: Missing type\n---\n", time.Time{})
	if !errors.As(err, &decodeErr) || decodeErr.Code != "okf.missing_type" {
		t.Fatalf("expected missing type decode error, got %v", err)
	}

	_, err = NewParser().ParseNote("bad", "---\ntype: [unterminated\n---\n", time.Time{})
	if !errors.As(err, &decodeErr) || decodeErr.Code != "okf.invalid_frontmatter" {
		t.Fatalf("expected invalid frontmatter decode error, got %v", err)
	}
}

func TestResolverResolvesAbsoluteRelativeWikiAndUnresolvedLinks(t *testing.T) {
	resolver := NewResolver([]domain.NoteID{"folder/source", "folder/beta", "root/topic", "Gamma Note"})
	links := []domain.ParsedLink{
		{RawTarget: "../root/topic.md", Kind: domain.LinkKindMarkdown, Strength: domain.LinkStrengthHard},
		{RawTarget: "beta.md", Kind: domain.LinkKindMarkdown, Strength: domain.LinkStrengthHard},
		{RawTarget: "/root/topic.md", Kind: domain.LinkKindMarkdown, Strength: domain.LinkStrengthHard},
		{RawTarget: "Gamma Note", Kind: domain.LinkKindWiki, Strength: domain.LinkStrengthHard},
		{RawTarget: "missing", Kind: domain.LinkKindWiki, Strength: domain.LinkStrengthHard},
		{RawTarget: "https://example.test", Kind: domain.LinkKindMarkdown, Strength: domain.LinkStrengthHard},
		{RawTarget: "", Heading: "section", Kind: domain.LinkKindMarkdown, Strength: domain.LinkStrengthHard},
	}
	resolved := resolver.ResolveLinks("folder/source", links)

	want := []*domain.NoteID{ptrID("root/topic"), ptrID("folder/beta"), ptrID("root/topic"), ptrID("Gamma Note"), nil, nil, ptrID("folder/source")}
	for i := range resolved {
		if !sameIDPtr(resolved[i].ResolvedID, want[i]) {
			t.Fatalf("link %d resolved to %#v, want %#v", i, resolved[i].ResolvedID, want[i])
		}
	}
}

func TestCodecEncodePreservesStandardAndUnknownMetadata(t *testing.T) {
	timestamp := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	doc, err := NewCodec().Encode(domain.OKFMetadata{
		Type:      "concept",
		Title:     "Encoded",
		Tags:      []domain.Tag{"okf", "go"},
		Timestamp: &timestamp,
		Unknown: map[string]any{
			"custom": "value",
		},
	}, "\n# Encoded\n")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	parsed, err := NewCodec().Decode("encoded", doc.Raw, time.Time{})
	if err != nil {
		t.Fatalf("decode encoded document: %v\n%s", err, doc.Raw)
	}
	if parsed.Metadata.Type != "concept" || parsed.Metadata.Title != "Encoded" {
		t.Fatalf("unexpected parsed metadata: %#v", parsed.Metadata)
	}
	if parsed.Metadata.Unknown["custom"] != "value" {
		t.Fatalf("expected unknown metadata to round trip, got %#v", parsed.Metadata.Unknown)
	}
	if len(parsed.Tags) != 2 || parsed.Tags[0] != "go" || parsed.Tags[1] != "okf" {
		t.Fatalf("unexpected tags: %#v", parsed.Tags)
	}
}
func assertLink(t *testing.T, link domain.ParsedLink, kind domain.LinkKind, raw string, display string, heading string) {
	t.Helper()
	if link.Kind != kind || link.RawTarget != raw || link.DisplayText != display || link.Heading != heading || link.Strength != domain.LinkStrengthHard {
		t.Fatalf("unexpected link: %#v", link)
	}
}

func ptrID(id domain.NoteID) *domain.NoteID {
	return &id
}

func sameIDPtr(left, right *domain.NoteID) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
