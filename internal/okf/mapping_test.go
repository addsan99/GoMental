package okf

import (
	"testing"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/ingest"
)

const mappingYAML = `
version: 1
rules:
  - match: "topics/**"
    defaults:
      type: topic
    searchAliases: [keywords]
    links:
      - field: depends_on
        strength: hard
        basePath: topics
      - field: relates_to
        strength: soft
        basePath: topics
`

func withMapping(t *testing.T) {
	t.Helper()
	m, err := ingest.Parse([]byte(mappingYAML))
	if err != nil {
		t.Fatalf("parse mapping: %v", err)
	}
	SetActiveMapping(m)
	t.Cleanup(func() { SetActiveMapping(ingest.Mapping{}) })
}

func TestParseNoteWithoutMappingIsUnchanged(t *testing.T) {
	SetActiveMapping(ingest.Mapping{})
	_, err := NewParser().ParseNote("topics/a", "no frontmatter here", time.Now())
	if err == nil {
		t.Fatal("expected missing frontmatter error without a mapping")
	}
	_, err = NewParser().ParseNote("topics/a", "---\ntitle: A\n---\nbody\n", time.Now())
	if err == nil {
		t.Fatal("expected missing type error without a mapping")
	}
}

func TestMappingSuppliesMissingType(t *testing.T) {
	withMapping(t)
	note, err := NewParser().ParseNote("topics/a", "---\ntitle: A\nkeywords: [pop, aruba]\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if note.Metadata.Type != "topic" {
		t.Fatalf("type = %q, want topic", note.Metadata.Type)
	}
	if note.Title != "A" {
		t.Fatalf("title = %q", note.Title)
	}
}

func TestFileTypeWinsOverDefault(t *testing.T) {
	withMapping(t)
	note, err := NewParser().ParseNote("topics/a", "---\ntype: procedure\ntitle: A\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if note.Metadata.Type != "procedure" {
		t.Fatalf("type = %q, want procedure (file wins)", note.Metadata.Type)
	}
}

func TestFrontmatterlessFileParsesWhenTypeDefaulted(t *testing.T) {
	withMapping(t)
	note, err := NewParser().ParseNote("topics/a", "# Heading\n\nprose\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if note.Metadata.Type != "topic" {
		t.Fatalf("type = %q", note.Metadata.Type)
	}
	if note.Body != "# Heading\n\nprose\n" {
		t.Fatalf("body = %q", note.Body)
	}
	if note.Title != "Heading" {
		t.Fatalf("title = %q, want Heading", note.Title)
	}
}

func TestUnclosedFrontmatterStillFails(t *testing.T) {
	withMapping(t)
	if _, err := NewParser().ParseNote("topics/a", "---\ntitle: A\nbody without close\n", time.Now()); err == nil {
		t.Fatal("unclosed frontmatter must remain an error")
	}
}

func TestDefaultsDoNotApplyOutsideGlob(t *testing.T) {
	withMapping(t)
	if _, err := NewParser().ParseNote("expertise/a", "---\ntitle: A\n---\nbody\n", time.Now()); err == nil {
		t.Fatal("expected missing type for a path the mapping does not cover")
	}
}

func TestMappedLinks(t *testing.T) {
	withMapping(t)
	raw := "---\ntitle: A\ndepends_on: [platform/pop.md]\nrelates_to:\n  - net/dns.md\n  - \"\"\n---\nbody\n"
	note, err := NewParser().ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Links) != 2 {
		t.Fatalf("links = %+v", note.Links)
	}
	if note.Links[0].RawTarget != "topics/platform/pop.md" || note.Links[0].Strength != domain.LinkStrengthHard {
		t.Fatalf("unexpected hard link: %+v", note.Links[0])
	}
	if note.Links[1].RawTarget != "topics/net/dns.md" || note.Links[1].Strength != domain.LinkStrengthSoft {
		t.Fatalf("unexpected soft link: %+v", note.Links[1])
	}
}

func TestMappedLinksAppendToBodyLinks(t *testing.T) {
	withMapping(t)
	raw := "---\ntitle: A\ndepends_on: [platform/pop.md]\n---\nSee [[topics/other]].\n"
	note, err := NewParser().ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Links) != 2 {
		t.Fatalf("expected body link plus mapped link, got %+v", note.Links)
	}
	if note.Links[0].RawTarget != "topics/other" {
		t.Fatalf("body link should come first: %+v", note.Links[0])
	}
}

func TestMappedTags(t *testing.T) {
	m, err := ingest.Parse([]byte("version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: topic\n    tagFields: [depth]\n"))
	if err != nil {
		t.Fatalf("parse mapping: %v", err)
	}
	SetActiveMapping(m)
	t.Cleanup(func() { SetActiveMapping(ingest.Mapping{}) })

	note, err := NewParser().ParseNote("topics/a", "---\ntitle: A\ndepth: deep-dive\ntags: [alpha]\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Tags) != 2 || note.Tags[0] != "alpha" || note.Tags[1] != "deep-dive" {
		t.Fatalf("tags = %v, want [alpha deep-dive]", note.Tags)
	}

	// A field the note does not have contributes nothing, and an authored tag
	// that already equals the mapped value is not duplicated.
	note, err = NewParser().ParseNote("topics/b", "---\ntitle: B\ntags: [hub]\ndepth: hub\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Tags) != 1 || note.Tags[0] != "hub" {
		t.Fatalf("tags = %v, want [hub]", note.Tags)
	}
}

func TestParserStripsLinkPrefixes(t *testing.T) {
	m, err := ingest.Parse([]byte("version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: topic\nstripLinkPrefixes:\n  - \"/base/dir\"\n"))
	if err != nil {
		t.Fatalf("parse mapping: %v", err)
	}
	SetActiveMapping(m)
	t.Cleanup(func() { SetActiveMapping(ingest.Mapping{}) })

	raw := "---\ntitle: A\n---\nSee [x](/base/dir/topics/other.md) and [y](/src/main.go).\n"
	note, err := NewParser().ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Links) != 2 {
		t.Fatalf("links = %+v", note.Links)
	}
	if note.Links[0].RawTarget != "/topics/other.md" {
		t.Fatalf("prefix not stripped: %q", note.Links[0].RawTarget)
	}
	if note.Links[1].RawTarget != "/src/main.go" {
		t.Fatalf("unrelated link was rewritten: %q", note.Links[1].RawTarget)
	}
}

func TestMappedAliases(t *testing.T) {
	withMapping(t)
	raw := "---\ntitle: A\nkeywords: [pop, aruba, pop]\n---\nbody\n"
	note, err := NewParser().ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Aliases) != 2 || note.Aliases[0] != "pop" || note.Aliases[1] != "aruba" {
		t.Fatalf("aliases = %v", note.Aliases)
	}
	doc := domain.SearchDocumentFromParsed(note, "topics/a.md")
	if len(doc.Aliases) != 2 {
		t.Fatalf("aliases should reach the search document, got %v", doc.Aliases)
	}
}
