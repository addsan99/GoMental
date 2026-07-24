package okf

import (
	"path"
	"strings"

	"GoMental/internal/domain"
)

type Resolver struct {
	ids map[string]domain.NoteID
}

func NewResolver(existing []domain.NoteID) Resolver {
	ids := make(map[string]domain.NoteID, len(existing))
	for _, id := range existing {
		ids[caseFold(string(id))] = id
	}
	return Resolver{ids: ids}
}

func (r Resolver) ResolveLinks(source domain.NoteID, links []domain.ParsedLink) []domain.ParsedLink {
	resolved := make([]domain.ParsedLink, len(links))
	for i, link := range links {
		resolved[i] = r.ResolveLink(source, link)
	}
	return resolved
}

func (r Resolver) ResolveLink(source domain.NoteID, link domain.ParsedLink) domain.ParsedLink {
	link.Source = source
	target := candidateTarget(source, link)
	if target == "" {
		return link
	}
	if id, ok := r.ids[caseFold(target)]; ok {
		resolved := id
		link.ResolvedID = &resolved
	}
	return link
}

func candidateTarget(source domain.NoteID, link domain.ParsedLink) string {
	raw := strings.TrimSpace(link.RawTarget)
	if raw == "" {
		if link.Heading != "" {
			return string(source)
		}
		return ""
	}
	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "mailto:") {
		return ""
	}
	raw, _ = splitTargetHeading(raw)
	absolute := strings.HasPrefix(raw, "/")
	raw = strings.TrimPrefix(raw, "/")
	raw = strings.ReplaceAll(raw, `\\`, "/")
	if strings.HasSuffix(strings.ToLower(raw), ".md") {
		raw = raw[:len(raw)-3]
	}
	if link.Kind == domain.LinkKindMarkdown && !absolute {
		base := path.Dir(string(source))
		if base == "." {
			base = ""
		}
		raw = path.Clean(path.Join(base, raw))
	} else {
		raw = path.Clean(raw)
	}
	if raw == "." || raw == ".." || strings.HasPrefix(raw, "../") {
		return ""
	}
	return raw
}

func caseFold(value string) string {
	return strings.ToLower(path.Clean(strings.ReplaceAll(value, `\\`, "/")))
}
