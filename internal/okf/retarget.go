package okf

import (
	"path"
	"strings"

	"GoMental/internal/domain"
)

// RetargetLinks rewrites every hard link in body that resolves to oldID so it
// points at newID instead, and reports whether anything changed.
//
// Matching runs through the ordinary Resolver, so a link is rewritten exactly
// when the graph counted it as a backlink of oldID — no second, drifting
// definition of what a link points at. source, oldID and newID must share an ID
// space (member-local on a composite workspace, where links never cross members).
func RetargetLinks(source domain.NoteID, body string, oldID, newID domain.NoteID) (string, bool) {
	if body == "" || oldID == newID {
		return body, false
	}
	resolver := NewResolver([]domain.NoteID{oldID})
	pointsAtOld := func(raw string, kind domain.LinkKind) bool {
		link := domain.ParsedLink{RawTarget: raw, Kind: kind}
		return resolver.ResolveLink(source, link).ResolvedID != nil
	}
	changed := false

	body = markdownLinkPattern.ReplaceAllStringFunc(body, func(match string) string {
		if strings.HasPrefix(match, "!") {
			return match
		}
		groups := markdownLinkPattern.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		rawTarget := groups[2]
		targetPath, heading := splitTargetHeading(rawTarget)
		if !pointsAtOld(targetPath, domain.LinkKindMarkdown) {
			return match
		}
		next := markdownNoteTarget(source, targetPath, newID)
		if heading != "" {
			next += "#" + heading
		}
		// Splice over the target span only so an optional link title survives.
		start := strings.Index(match, "](")
		if start < 0 {
			return match
		}
		start += len("](")
		changed = true
		return match[:start] + next + match[start+len(rawTarget):]
	})

	body = wikiLinkPattern.ReplaceAllStringFunc(body, func(match string) string {
		groups := wikiLinkPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		rawTarget := groups[1]
		if !pointsAtOld(strings.TrimSpace(rawTarget), domain.LinkKindWiki) {
			return match
		}
		changed = true
		// Wiki targets always resolve from the workspace root, so the new ID is
		// the target verbatim. Splicing group 1 keeps any heading and alias.
		next := preserveMarkdownSuffix(rawTarget, string(newID))
		return "[[" + next + match[len("[[")+len(rawTarget):]
	})
	return body, changed
}

// markdownNoteTarget renders newID the way the original link addressed its
// target: root-anchored links stay root-anchored, relative ones stay relative.
func markdownNoteTarget(source domain.NoteID, rawTarget string, newID domain.NoteID) string {
	if strings.HasPrefix(rawTarget, "/") {
		return "/" + preserveMarkdownSuffix(rawTarget, string(newID))
	}
	return preserveMarkdownSuffix(rawTarget, relativeNoteRef(path.Dir(string(source)), string(newID)))
}

// relativeNoteRef expresses target relative to fromDir in note-ID space, which
// is how candidateTarget resolves a relative Markdown link back to an ID.
func relativeNoteRef(fromDir, target string) string {
	if fromDir == "." || fromDir == "" {
		return target
	}
	fromParts := strings.Split(fromDir, "/")
	toParts := strings.Split(target, "/")
	common := 0
	// Stop before the final target segment: that is the note name, never a
	// shared directory.
	for common < len(fromParts) && common < len(toParts)-1 && fromParts[common] == toParts[common] {
		common++
	}
	parts := make([]string, 0, (len(fromParts)-common)+(len(toParts)-common))
	for i := common; i < len(fromParts); i++ {
		parts = append(parts, "..")
	}
	parts = append(parts, toParts[common:]...)
	return strings.Join(parts, "/")
}

func preserveMarkdownSuffix(original, next string) string {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(original)), ".md") {
		return next + ".md"
	}
	return next
}
