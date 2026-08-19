// Package ingest implements the optional per-workspace ingest profile stored at
// <root>/.gomental/mapping.yaml.
//
// The profile lets a workspace describe how a foreign Markdown corpus maps onto
// GoMental's existing concepts, so that corpus can be read without any
// corpus-specific logic in the core. It can:
//
//   - supply frontmatter defaults for files that lack them (e.g. a required `type`),
//   - declare frontmatter list fields that are really note-to-note links,
//   - declare frontmatter fields whose values should be searchable as aliases,
//   - exclude paths from ingestion entirely.
//
// When no mapping.yaml exists, Load returns a zero Mapping whose methods are all
// no-ops, so behaviour is byte-for-byte identical to having no profile at all.
package ingest

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the workspace-relative location of the ingest profile.
var FileName = filepath.Join(".gomental", "mapping.yaml")

// Link strengths a mapped link may take. Plain strings so this package stays
// free of domain dependencies.
const (
	StrengthHard = "hard"
	StrengthSoft = "soft"
)

// LinkField declares that a frontmatter list field holds note references.
type LinkField struct {
	// Field is the frontmatter key, e.g. "depends_on".
	Field string `yaml:"field"`
	// Strength is "hard" (default) or "soft".
	Strength string `yaml:"strength"`
	// BasePath is prepended to every reference before resolution, e.g. "topics"
	// when references are written relative to a subdirectory.
	BasePath string `yaml:"basePath"`
}

// Rule applies to every note whose id matches Match.
type Rule struct {
	// Match is a glob against the note id (not the file path), e.g. "topics/**".
	Match string `yaml:"match"`
	// Defaults are frontmatter values applied only when the key is absent.
	Defaults map[string]string `yaml:"defaults"`
	// SearchAliases lists frontmatter fields whose values are indexed as aliases.
	SearchAliases []string `yaml:"searchAliases"`
	// TagFields lists frontmatter fields whose values become ordinary tags.
	// Only use it for low-cardinality fields: every value becomes a graph hub
	// node, so a field where most values are unique adds noise, not structure.
	TagFields []string `yaml:"tagFields"`
	// Links declares frontmatter fields that hold note references.
	Links []LinkField `yaml:"links"`
}

// Mapping is a parsed ingest profile. The zero value is a valid no-op profile.
type Mapping struct {
	Version int      `yaml:"version"`
	Rules   []Rule   `yaml:"rules"`
	Exclude []string `yaml:"exclude"`
	// StripLinkPrefixes are leading path prefixes removed from link targets
	// before resolution. Corpora that live in a subdirectory of a repo often
	// write repo-root-absolute links, which point outside the workspace unless
	// the corpus prefix is stripped.
	StripLinkPrefixes []string `yaml:"stripLinkPrefixes"`
}

// IsZero reports whether the mapping would change nothing.
func (m Mapping) IsZero() bool {
	return len(m.Rules) == 0 && len(m.Exclude) == 0 && len(m.StripLinkPrefixes) == 0
}

// RewriteTarget strips a declared prefix from a link target. Targets that match
// no prefix are returned unchanged, so unrelated links keep their meaning.
func (m Mapping) RewriteTarget(target string) string {
	if len(m.StripLinkPrefixes) == 0 {
		return target
	}
	trimmed := strings.TrimSpace(target)
	if trimmed == "" || strings.Contains(trimmed, "://") {
		return target
	}
	for _, prefix := range m.StripLinkPrefixes {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.TrimPrefix(trimmed, prefix)
		if rest == "" {
			continue
		}
		if !strings.HasPrefix(rest, "/") {
			continue
		}
		return rest
	}
	return target
}

// Load reads the ingest profile for a workspace root. A missing file is not an
// error: it yields the zero Mapping.
func Load(root string) (Mapping, error) {
	raw, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Mapping{}, nil
		}
		return Mapping{}, err
	}
	return Parse(raw)
}

// Parse decodes an ingest profile from YAML.
func Parse(raw []byte) (Mapping, error) {
	var m Mapping
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return Mapping{}, err
	}
	m.normalize()
	return m, nil
}

func (m *Mapping) normalize() {
	rules := make([]Rule, 0, len(m.Rules))
	for _, rule := range m.Rules {
		rule.Match = strings.TrimSpace(rule.Match)
		if rule.Match == "" {
			continue
		}
		links := make([]LinkField, 0, len(rule.Links))
		for _, link := range rule.Links {
			link.Field = strings.TrimSpace(link.Field)
			if link.Field == "" {
				continue
			}
			if link.Strength != StrengthSoft {
				link.Strength = StrengthHard
			}
			link.BasePath = strings.Trim(strings.TrimSpace(link.BasePath), "/")
			links = append(links, link)
		}
		rule.Links = links
		rule.SearchAliases = trimAll(rule.SearchAliases)
		rule.TagFields = trimAll(rule.TagFields)
		rules = append(rules, rule)
	}
	m.Rules = rules
	m.Exclude = trimAll(m.Exclude)
	prefixes := make([]string, 0, len(m.StripLinkPrefixes))
	for _, prefix := range trimAll(m.StripLinkPrefixes) {
		prefixes = append(prefixes, strings.TrimRight(prefix, "/"))
	}
	m.StripLinkPrefixes = prefixes
}

func trimAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Excluded reports whether a note id is excluded from ingestion.
func (m Mapping) Excluded(noteID string) bool {
	for _, pattern := range m.Exclude {
		if matchGlob(pattern, noteID) {
			return true
		}
	}
	return false
}

// rulesFor returns every rule matching a note id, in declaration order.
func (m Mapping) rulesFor(noteID string) []Rule {
	var out []Rule
	for _, rule := range m.Rules {
		if matchGlob(rule.Match, noteID) {
			out = append(out, rule)
		}
	}
	return out
}

// Defaults returns the frontmatter defaults that apply to a note id. Earlier
// rules win when several rules set the same key.
func (m Mapping) Defaults(noteID string) map[string]string {
	var out map[string]string
	for _, rule := range m.rulesFor(noteID) {
		for key, value := range rule.Defaults {
			if out == nil {
				out = map[string]string{}
			}
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	return out
}

// AliasFields returns the frontmatter fields to index as search aliases.
func (m Mapping) AliasFields(noteID string) []string {
	return m.fieldsFor(noteID, func(r Rule) []string { return r.SearchAliases })
}

// TagFields returns the frontmatter fields whose values become tags.
func (m Mapping) TagFields(noteID string) []string {
	return m.fieldsFor(noteID, func(r Rule) []string { return r.TagFields })
}

func (m Mapping) fieldsFor(noteID string, pick func(Rule) []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, rule := range m.rulesFor(noteID) {
		for _, field := range pick(rule) {
			if _, ok := seen[field]; ok {
				continue
			}
			seen[field] = struct{}{}
			out = append(out, field)
		}
	}
	return out
}

// LinkFields returns the frontmatter link declarations for a note id.
func (m Mapping) LinkFields(noteID string) []LinkField {
	var out []LinkField
	seen := map[string]struct{}{}
	for _, rule := range m.rulesFor(noteID) {
		for _, link := range rule.Links {
			if _, ok := seen[link.Field]; ok {
				continue
			}
			seen[link.Field] = struct{}{}
			out = append(out, link)
		}
	}
	return out
}

// Target applies a link field's base path to a raw reference.
func (f LinkField) Target(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, `\`, "/")
	if f.BasePath == "" {
		return raw
	}
	return path.Join(f.BasePath, raw)
}

// matchGlob matches a slash-separated path against a glob supporting "**"
// (any number of segments) and "*" (within one segment).
func matchGlob(pattern, value string) bool {
	pattern = strings.Trim(strings.TrimSpace(pattern), "/")
	value = strings.Trim(strings.ReplaceAll(value, `\`, "/"), "/")
	if pattern == "" {
		return false
	}
	if pattern == "**" {
		return true
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(value, "/"))
}

func matchSegments(pattern, value []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			if len(pattern) == 1 {
				// A trailing "**" matches one or more segments, so "topics/**"
				// covers everything under topics/ without matching the folder
				// note "topics.md" itself.
				return len(value) > 0
			}
			for i := 0; i <= len(value); i++ {
				if matchSegments(pattern[1:], value[i:]) {
					return true
				}
			}
			return false
		}
		if len(value) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], value[0]); err != nil || !ok {
			return false
		}
		pattern, value = pattern[1:], value[1:]
	}
	return len(value) == 0
}
