// Package composite holds the parsing and link-resolution rules that differ
// between an ordinary workspace and a composite one.
//
// Both differences come from the same fact: on a composite, a note ID is
// namespaced with the member that owns it. That means the ingest profile used
// to parse a note varies per note, and wiki-links have to be resolved against
// the member's own IDs rather than the composite's.
package composite

import (
	"GoMental/internal/domain"
	"GoMental/internal/okf"
	"GoMental/internal/workspace"
)

// CodecFor returns the codec that should decode a given note.
func CodecFor(ws workspace.Workspace, id domain.NoteID) okf.Codec {
	return okf.NewCodecWithMapping(ws.MappingForNoteID(id))
}

// ResolveAll resolves wiki-links for a whole corpus in place.
//
// On a composite each member resolves against its own un-namespaced IDs and the
// results are namespaced afterwards, so [[Foo]] means the Foo in the writer's
// own workspace. Links across members deliberately stay unresolved: members are
// joined through shared tag, type and heading hubs, not by assuming that
// same-named notes in different workspaces are the same note.
func ResolveAll(ws workspace.Workspace, parsed []domain.ParsedOKFNote) {
	if !ws.IsComposite() {
		ids := make([]domain.NoteID, 0, len(parsed))
		for _, note := range parsed {
			ids = append(ids, note.ID)
		}
		resolver := okf.NewResolver(ids)
		for i := range parsed {
			parsed[i].Links = resolver.ResolveLinks(parsed[i].ID, parsed[i].Links)
		}
		return
	}

	localIDs := make([]domain.NoteID, len(parsed))
	byPrefix := make(map[string][]int, len(ws.Members()))
	for i, note := range parsed {
		member, localID, err := ws.MemberForNoteID(note.ID)
		if err != nil {
			continue
		}
		localIDs[i] = localID
		byPrefix[member.Prefix] = append(byPrefix[member.Prefix], i)
	}

	for prefix, indexes := range byPrefix {
		candidates := make([]domain.NoteID, 0, len(indexes))
		for _, i := range indexes {
			candidates = append(candidates, localIDs[i])
		}
		resolver := okf.NewResolver(candidates)
		for _, i := range indexes {
			parsed[i].Links = namespaceResolved(prefix, resolver.ResolveLinks(localIDs[i], parsed[i].Links))
		}
	}
}

// ResolveOne resolves the links of a single note against a set of candidate IDs
// drawn from the workspace (already namespaced on a composite).
func ResolveOne(ws workspace.Workspace, source domain.NoteID, links []domain.ParsedLink, candidates []domain.NoteID) []domain.ParsedLink {
	if !ws.IsComposite() {
		return okf.NewResolver(candidates).ResolveLinks(source, links)
	}
	member, localSource, err := ws.MemberForNoteID(source)
	if err != nil {
		return links
	}
	local := make([]domain.NoteID, 0, len(candidates))
	for _, candidate := range candidates {
		candidateMember, localID, err := ws.MemberForNoteID(candidate)
		if err != nil || candidateMember.Prefix != member.Prefix {
			continue
		}
		local = append(local, localID)
	}
	resolved := okf.NewResolver(local).ResolveLinks(localSource, links)
	return namespaceResolved(member.Prefix, resolved)
}

func namespaceResolved(prefix string, links []domain.ParsedLink) []domain.ParsedLink {
	for i := range links {
		if links[i].ResolvedID == nil {
			continue
		}
		namespaced := domain.NoteID(prefix + "/" + string(*links[i].ResolvedID))
		links[i].ResolvedID = &namespaced
	}
	return links
}
