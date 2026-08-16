package okf

import (
	"fmt"
	"sort"

	"GoMental/internal/domain"
	"GoMental/internal/ingest"
)

// The ingest profile is carried by the Parser that is using it, not by process
// state. A workspace's profile therefore travels with the parser that reads its
// notes, which is what lets several workspaces — each with its own profile, or
// none — be parsed at the same time inside one process.
//
// A zero mapping is the default and every accessor below is a no-op under it,
// which is what keeps stock workspaces behaving exactly as they always have.

// applyDefaults fills frontmatter fields the file left unset. Only fields that
// are genuinely absent are touched, so a file always wins over the profile.
// hasDefaultType reports whether the ingest profile supplies a `type` for this
// note id, which is what makes a frontmatter-less file ingestible.
func (p Parser) hasDefaultType(id domain.NoteID) bool {
	return p.mapping.Defaults(string(id))["type"] != ""
}

func (p Parser) applyDefaults(id domain.NoteID, metadata *domain.OKFMetadata) {
	defaults := p.mapping.Defaults(string(id))
	if len(defaults) == 0 {
		return
	}
	for key, value := range defaults {
		switch key {
		case "type":
			if metadata.Type == "" {
				metadata.Type = value
			}
		case "title":
			if metadata.Title == "" {
				metadata.Title = value
			}
		case "description":
			if metadata.Description == "" {
				metadata.Description = value
			}
		case "resource":
			if metadata.Resource == "" {
				metadata.Resource = value
			}
		default:
			if metadata.Unknown == nil {
				metadata.Unknown = map[string]any{}
			}
			if _, exists := metadata.Unknown[key]; !exists {
				metadata.Unknown[key] = value
			}
		}
	}
}

// mappedLinks turns declared frontmatter reference fields into parsed links.
// They are ordinary links from that point on: the existing resolver, graph
// builder and backlink queries need no knowledge of where they came from.
func (p Parser) mappedLinks(id domain.NoteID, metadata domain.OKFMetadata) []domain.ParsedLink {
	fields := p.mapping.LinkFields(string(id))
	if len(fields) == 0 || len(metadata.Unknown) == 0 {
		return nil
	}
	var links []domain.ParsedLink
	for _, field := range fields {
		for _, raw := range stringList(metadata.Unknown[field.Field]) {
			target := field.Target(raw)
			if target == "" {
				continue
			}
			strength := domain.LinkStrengthHard
			if field.Strength == ingest.StrengthSoft {
				strength = domain.LinkStrengthSoft
			}
			links = append(links, domain.ParsedLink{
				Source:      id,
				RawTarget:   target,
				DisplayText: raw,
				Kind:        domain.LinkKindWiki,
				Strength:    strength,
			})
		}
	}
	return links
}

// applyMappedTags folds declared frontmatter fields into the note's tags. They
// go through the same parseTags normalization as authored tags, so from here on
// nothing can tell them apart: the tag facet, graph hubs, filters and search all
// treat them as ordinary tags.
func (p Parser) applyMappedTags(id domain.NoteID, metadata *domain.OKFMetadata) {
	fields := p.mapping.TagFields(string(id))
	if len(fields) == 0 || len(metadata.Unknown) == 0 {
		return
	}
	existing := map[domain.Tag]struct{}{}
	for _, tag := range metadata.Tags {
		existing[tag] = struct{}{}
	}
	for _, field := range fields {
		value, ok := metadata.Unknown[field]
		if !ok {
			continue
		}
		for _, tag := range parseTags(value) {
			if _, seen := existing[tag]; seen {
				continue
			}
			existing[tag] = struct{}{}
			metadata.Tags = append(metadata.Tags, tag)
		}
	}
	sort.Slice(metadata.Tags, func(i, j int) bool { return metadata.Tags[i] < metadata.Tags[j] })
}

// rewriteTargets applies the profile's declared link-prefix strips. Links that
// match nothing are returned untouched.
func (p Parser) rewriteTargets(links []domain.ParsedLink) []domain.ParsedLink {
	mapping := p.mapping
	if len(mapping.StripLinkPrefixes) == 0 {
		return links
	}
	for i := range links {
		links[i].RawTarget = mapping.RewriteTarget(links[i].RawTarget)
	}
	return links
}

// mappedAliases collects the values of declared alias fields so they land in the
// search index's existing (and already boosted) aliases field.
func (p Parser) mappedAliases(id domain.NoteID, metadata domain.OKFMetadata) []string {
	fields := p.mapping.AliasFields(string(id))
	if len(fields) == 0 || len(metadata.Unknown) == 0 {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, field := range fields {
		for _, value := range stringList(metadata.Unknown[field]) {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

// stringList coerces a YAML scalar or sequence into a list of non-empty strings.
func stringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := scalarString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		if s := fmt.Sprint(typed); s != "" {
			return []string{s}
		}
		return nil
	}
}
