package okf

import (
	"fmt"
	"sync"
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

func mappedParser(t *testing.T) Parser {
	t.Helper()
	m, err := ingest.Parse([]byte(mappingYAML))
	if err != nil {
		t.Fatalf("parse mapping: %v", err)
	}
	return NewParserWithMapping(m)
}

func parserFor(t *testing.T, yaml string) Parser {
	t.Helper()
	m, err := ingest.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse mapping: %v", err)
	}
	return NewParserWithMapping(m)
}

func TestParseNoteWithoutMappingIsUnchanged(t *testing.T) {
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
	parser := mappedParser(t)
	note, err := parser.ParseNote("topics/a", "---\ntitle: A\nkeywords: [pop, aruba]\n---\nbody\n", time.Now())
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
	parser := mappedParser(t)
	note, err := parser.ParseNote("topics/a", "---\ntype: procedure\ntitle: A\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if note.Metadata.Type != "procedure" {
		t.Fatalf("type = %q, want procedure (file wins)", note.Metadata.Type)
	}
}

func TestFrontmatterlessFileParsesWhenTypeDefaulted(t *testing.T) {
	parser := mappedParser(t)
	note, err := parser.ParseNote("topics/a", "# Heading\n\nprose\n", time.Now())
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
	parser := mappedParser(t)
	if _, err := parser.ParseNote("topics/a", "---\ntitle: A\nbody without close\n", time.Now()); err == nil {
		t.Fatal("unclosed frontmatter must remain an error")
	}
}

func TestDefaultsDoNotApplyOutsideGlob(t *testing.T) {
	parser := mappedParser(t)
	if _, err := parser.ParseNote("expertise/a", "---\ntitle: A\n---\nbody\n", time.Now()); err == nil {
		t.Fatal("expected missing type for a path the mapping does not cover")
	}
}

func TestMappedLinks(t *testing.T) {
	parser := mappedParser(t)
	raw := "---\ntitle: A\ndepends_on: [platform/pop.md]\nrelates_to:\n  - net/dns.md\n  - \"\"\n---\nbody\n"
	note, err := parser.ParseNote("topics/a", raw, time.Now())
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
	parser := mappedParser(t)
	raw := "---\ntitle: A\ndepends_on: [platform/pop.md]\n---\nSee [[topics/other]].\n"
	note, err := parser.ParseNote("topics/a", raw, time.Now())
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
	parser := parserFor(t, "version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: topic\n    tagFields: [depth]\n")

	note, err := parser.ParseNote("topics/a", "---\ntitle: A\ndepth: deep-dive\ntags: [alpha]\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Tags) != 2 || note.Tags[0] != "alpha" || note.Tags[1] != "deep-dive" {
		t.Fatalf("tags = %v, want [alpha deep-dive]", note.Tags)
	}

	// A field the note does not have contributes nothing, and an authored tag
	// that already equals the mapped value is not duplicated.
	note, err = parser.ParseNote("topics/b", "---\ntitle: B\ntags: [hub]\ndepth: hub\n---\nbody\n", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(note.Tags) != 1 || note.Tags[0] != "hub" {
		t.Fatalf("tags = %v, want [hub]", note.Tags)
	}
}

func TestParserStripsLinkPrefixes(t *testing.T) {
	parser := parserFor(t, "version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: topic\nstripLinkPrefixes:\n  - \"/base/dir\"\n")

	raw := "---\ntitle: A\n---\nSee [x](/base/dir/topics/other.md) and [y](/src/main.go).\n"
	note, err := parser.ParseNote("topics/a", raw, time.Now())
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
	parser := mappedParser(t)
	raw := "---\ntitle: A\nkeywords: [pop, aruba, pop]\n---\nbody\n"
	note, err := parser.ParseNote("topics/a", raw, time.Now())
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

// The profile used to be process state, which quietly meant one workspace at a
// time. Two parsers must now be able to read the same note id under different
// profiles simultaneously, because that is what lets a composite workspace hold
// members that were ingested differently.
func TestParsersHoldIndependentMappings(t *testing.T) {
	topics := parserFor(t, "version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: topic\n")
	expertise := parserFor(t, "version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: expertise\n")
	plain := NewParser()

	raw := "---\ntitle: A\n---\nbody\n"
	first, err := topics.ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("topics parse: %v", err)
	}
	second, err := expertise.ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("expertise parse: %v", err)
	}
	if first.Metadata.Type != "topic" || second.Metadata.Type != "expertise" {
		t.Fatalf("profiles bled into each other: %q and %q", first.Metadata.Type, second.Metadata.Type)
	}
	// Re-parsing with the first parser must still yield the first profile.
	again, err := topics.ParseNote("topics/a", raw, time.Now())
	if err != nil {
		t.Fatalf("topics reparse: %v", err)
	}
	if again.Metadata.Type != "topic" {
		t.Fatalf("type = %q after the second parser ran, want topic", again.Metadata.Type)
	}
	if _, err := plain.ParseNote("topics/a", raw, time.Now()); err == nil {
		t.Fatal("a parser with no profile should still reject a note with no type")
	}
}

func TestParsersWithDifferentMappingsAreConcurrencySafe(t *testing.T) {
	topics := parserFor(t, "version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: topic\n")
	expertise := parserFor(t, "version: 1\nrules:\n  - match: \"topics/**\"\n    defaults:\n      type: expertise\n")
	raw := "---\ntitle: A\n---\nbody\n"

	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			note, err := topics.ParseNote("topics/a", raw, time.Now())
			if err != nil {
				errs <- err
			} else if note.Metadata.Type != "topic" {
				errs <- fmt.Errorf("topic parser produced %q", note.Metadata.Type)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			note, err := expertise.ParseNote("topics/a", raw, time.Now())
			if err != nil {
				errs <- err
			} else if note.Metadata.Type != "expertise" {
				errs <- fmt.Errorf("expertise parser produced %q", note.Metadata.Type)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent parse: %v", err)
	}
}
