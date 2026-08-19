package ingest_test

import (
	"os"
	"path/filepath"
	"testing"

	"GoMental/internal/ingest"
)

const sample = `
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
  - match: "expertise/**"
    defaults:
      type: expertise
exclude:
  - "topics/feature-flags/**"
`

func load(t *testing.T) ingest.Mapping {
	t.Helper()
	m, err := ingest.Parse([]byte(sample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

func TestZeroMappingIsInert(t *testing.T) {
	var m ingest.Mapping
	if !m.IsZero() {
		t.Fatal("zero mapping should report IsZero")
	}
	if m.Excluded("anything") {
		t.Fatal("zero mapping should exclude nothing")
	}
	if len(m.Defaults("topics/x")) != 0 || len(m.LinkFields("topics/x")) != 0 || len(m.AliasFields("topics/x")) != 0 {
		t.Fatal("zero mapping should contribute nothing")
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	m, err := ingest.Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !m.IsZero() {
		t.Fatal("missing mapping.yaml should yield the zero mapping")
	}
}

func TestLoadFromWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gomental"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ingest.FileName), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ingest.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Defaults("topics/platform/pop")["type"] != "topic" {
		t.Fatalf("expected topic default, got %v", m.Defaults("topics/platform/pop"))
	}
}

func TestDefaultsScopedByGlob(t *testing.T) {
	m := load(t)
	if got := m.Defaults("topics/platform/pop")["type"]; got != "topic" {
		t.Fatalf("topics: got %q", got)
	}
	if got := m.Defaults("expertise/go-code")["type"]; got != "expertise" {
		t.Fatalf("expertise: got %q", got)
	}
	if got := m.Defaults("procedures/thing"); len(got) != 0 {
		t.Fatalf("unmatched id should get no defaults, got %v", got)
	}
}

func TestExcludeGlob(t *testing.T) {
	m := load(t)
	if !m.Excluded("topics/feature-flags/feature-flag---x") {
		t.Fatal("feature-flags should be excluded")
	}
	if m.Excluded("topics/platform/pop") {
		t.Fatal("other topics should not be excluded")
	}
}

func TestLinkFieldsAndBasePath(t *testing.T) {
	m := load(t)
	fields := m.LinkFields("topics/platform/pop")
	if len(fields) != 2 {
		t.Fatalf("expected 2 link fields, got %d", len(fields))
	}
	if fields[0].Field != "depends_on" || fields[0].Strength != ingest.StrengthHard {
		t.Fatalf("unexpected first field: %+v", fields[0])
	}
	if fields[1].Strength != ingest.StrengthSoft {
		t.Fatalf("relates_to should be soft, got %q", fields[1].Strength)
	}
	if got := fields[0].Target("platform/overview.md"); got != "topics/platform/overview.md" {
		t.Fatalf("base path not applied: %q", got)
	}
	if got := fields[0].Target("  "); got != "" {
		t.Fatalf("blank ref should yield empty target, got %q", got)
	}
}

func TestAliasFields(t *testing.T) {
	m := load(t)
	if got := m.AliasFields("topics/platform/pop"); len(got) != 1 || got[0] != "keywords" {
		t.Fatalf("unexpected alias fields: %v", got)
	}
	if got := m.AliasFields("expertise/go-code"); len(got) != 0 {
		t.Fatalf("expertise declares no aliases, got %v", got)
	}
}

func TestTagFields(t *testing.T) {
	m, err := ingest.Parse([]byte("version: 1\nrules:\n  - match: \"topics/**\"\n    tagFields: [depth, depth, \" status \"]\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := m.TagFields("topics/a")
	if len(got) != 2 || got[0] != "depth" || got[1] != "status" {
		t.Fatalf("tag fields = %v, want [depth status]", got)
	}
	if got := m.TagFields("expertise/a"); len(got) != 0 {
		t.Fatalf("unmatched id should get no tag fields, got %v", got)
	}
	var zero ingest.Mapping
	if got := zero.TagFields("topics/a"); len(got) != 0 {
		t.Fatalf("zero mapping should declare no tag fields, got %v", got)
	}
}

func TestStripLinkPrefixes(t *testing.T) {
	m, err := ingest.Parse([]byte("version: 1\nstripLinkPrefixes:\n  - \"/.github/copilot-instructions\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct{ in, want string }{
		{"/.github/copilot-instructions/topics/a.md", "/topics/a.md"},
		{"/src/services/auth", "/src/services/auth"},
		{"/.github/copilot-instructions", "/.github/copilot-instructions"},
		{"/.github/copilot-instructions-other/x", "/.github/copilot-instructions-other/x"},
		{"https://example.com/.github/copilot-instructions/x", "https://example.com/.github/copilot-instructions/x"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := m.RewriteTarget(tc.in); got != tc.want {
			t.Errorf("RewriteTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	var zero ingest.Mapping
	if got := zero.RewriteTarget("/.github/copilot-instructions/x"); got != "/.github/copilot-instructions/x" {
		t.Errorf("zero mapping rewrote target: %q", got)
	}
	if !zero.IsZero() {
		t.Error("zero mapping should still be zero")
	}
	if m.IsZero() {
		t.Error("a mapping with only strip prefixes is not inert")
	}
}

func TestGlobMatching(t *testing.T) {
	cases := []struct {
		pattern, value string
		want           bool
	}{
		{"**", "a/b/c", true},
		{"topics/**", "topics/a/b", true},
		{"topics/**", "topics", false},
		{"topics/*", "topics/a", true},
		{"topics/*", "topics/a/b", false},
		{"topics/feature-flags/**", "topics/feature-flags/x", true},
		{"topics/feature-flags/**", "topics/platform/x", false},
		{"a/**/c", "a/b/c", true},
		{"a/**/c", "a/b/d/c", true},
		{"a/**/c", "a/c", true},
	}
	for _, tc := range cases {
		m := ingest.Mapping{Exclude: []string{tc.pattern}}
		if got := m.Excluded(tc.value); got != tc.want {
			t.Errorf("match(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}
