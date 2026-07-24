package graph

import (
	"strings"

	"GoMental/internal/domain"
)

// noteMeta holds the per-note fields soft inference needs. Since soft inference is
// title-only (faceted equality moved to metadata hub links), scoring needs just the
// candidate's Title and the source's inferText = lower(PlainText+" "+Body) — the
// exact text inferEvidence scans for title mentions.
type noteMeta struct {
	ID         domain.NoteID
	Title      string
	titleLower string // trimmed + lowercased; "" when the title is blank
	inferText  string
}

func newNoteMeta(note domain.ParsedOKFNote) *noteMeta {
	return &noteMeta{
		ID:         note.ID,
		Title:      note.Title,
		titleLower: strings.ToLower(strings.TrimSpace(note.Title)),
		inferText:  strings.ToLower(note.PlainText + " " + note.Body),
	}
}

func (m *noteMeta) toParsed() domain.ParsedOKFNote {
	// PlainText carries the already-lowered inferText and Body is empty; inferEvidence
	// re-lowers (idempotent), so the scanned text equals the original note's.
	return domain.ParsedOKFNote{ID: m.ID, Title: m.Title, PlainText: m.inferText}
}

// headingKey normalizes a heading to the key sharedHeadings historically compared
// on (lowercase, no trim). Used by the heading metadata facet (facets.go).
func headingKey(h domain.Heading) string {
	return strings.ToLower(h.Text)
}

// CorpusIndex is an in-memory projection of a note corpus supporting exact,
// sub-quadratic soft-link inference. The title matcher generates a COMPLETE
// candidate set: any note whose title is not a substring of a source's text scores
// exactly 0 in the (title-only) inferEvidence, so it can be skipped without
// changing results. The matcher is rebuilt lazily when the corpus mutates (dirty).
type CorpusIndex struct {
	order   []domain.NoteID
	byID    map[domain.NoteID]*noteMeta
	matcher *titleMatcher
	dirty   bool
}

// BuildCorpusIndex constructs an index over the parsed corpus.
func BuildCorpusIndex(corpus []domain.ParsedOKFNote) *CorpusIndex {
	idx := &CorpusIndex{byID: make(map[domain.NoteID]*noteMeta, len(corpus))}
	for _, note := range corpus {
		idx.order = append(idx.order, note.ID)
		idx.byID[note.ID] = newNoteMeta(note)
	}
	idx.rebuildMatcher()
	return idx
}

func (idx *CorpusIndex) rebuildMatcher() {
	patterns := map[string][]domain.NoteID{}
	for id, m := range idx.byID {
		if m.titleLower == "" {
			continue
		}
		patterns[m.titleLower] = append(patterns[m.titleLower], id)
	}
	idx.matcher = newTitleMatcher(patterns)
	idx.dirty = false
}

// ResolverIDs returns a copy of the note ID set, for building an okf.Resolver
// without a filesystem walk.
func (idx *CorpusIndex) ResolverIDs() []domain.NoteID {
	ids := make([]domain.NoteID, len(idx.order))
	copy(ids, idx.order)
	return ids
}

// Upsert inserts or replaces a note's metadata. The title matcher is marked dirty
// and rebuilt lazily on the next read that needs it (InferAll), so a burst of
// saves costs O(1) each amortized.
func (idx *CorpusIndex) Upsert(note domain.ParsedOKFNote) {
	if _, ok := idx.byID[note.ID]; !ok {
		idx.order = append(idx.order, note.ID)
	}
	idx.byID[note.ID] = newNoteMeta(note)
	idx.dirty = true
}

// Delete removes a note from the index.
func (idx *CorpusIndex) Delete(id domain.NoteID) {
	if _, ok := idx.byID[id]; !ok {
		return
	}
	delete(idx.byID, id)
	for i, existing := range idx.order {
		if existing == id {
			idx.order = append(idx.order[:i], idx.order[i+1:]...)
			break
		}
	}
	idx.dirty = true
}

// Clone returns a deep-enough copy for concurrent reads: order and byID maps are
// copied, but *noteMeta values are shared (they are never mutated in place —
// Upsert replaces the pointer). The clone's matcher is left to rebuild lazily so
// the caller can run InferAll on it without touching the source index.
func (idx *CorpusIndex) Clone() *CorpusIndex {
	order := make([]domain.NoteID, len(idx.order))
	copy(order, idx.order)
	byID := make(map[domain.NoteID]*noteMeta, len(idx.byID))
	for id, m := range idx.byID {
		byID[id] = m
	}
	return &CorpusIndex{order: order, byID: byID, dirty: true}
}

// ComputeAffectedSources returns the minimal set of source notes whose soft
// out-links can change given a batch of changed/deleted notes, relative to the prev
// snapshot. Soft inference is title-only, so an edge u->v changes iff u's text
// changed (u in changed) or v's title changed/was removed (v in changed/deleted).
// Hence: every changed note (its own out-links), plus every note whose text
// mentions the old-or-new title of any changed/deleted note (edges into those). A
// note outside this set has identical inputs to every f(u,·) and is left untouched
// — so recomputing only these sources yields the same result as a full rebuild.
func ComputeAffectedSources(cur, prev *CorpusIndex, changed, deleted []domain.NoteID) []domain.NoteID {
	affected := map[domain.NoteID]struct{}{}
	for _, id := range changed {
		if _, ok := cur.byID[id]; ok {
			affected[id] = struct{}{}
		}
	}
	titles := map[string]struct{}{}
	addTitle := func(idx *CorpusIndex, id domain.NoteID) {
		if idx == nil {
			return
		}
		if m, ok := idx.byID[id]; ok && m.titleLower != "" {
			titles[m.titleLower] = struct{}{}
		}
	}
	for _, id := range changed {
		addTitle(cur, id)
		addTitle(prev, id)
	}
	for _, id := range deleted {
		addTitle(prev, id)
	}
	if len(titles) > 0 {
		patterns := make(map[string][]domain.NoteID, len(titles))
		for t := range titles {
			patterns[t] = []domain.NoteID{"_"}
		}
		matcher := newTitleMatcher(patterns)
		for _, id := range cur.order {
			m := cur.byID[id]
			if m == nil {
				continue
			}
			if len(matcher.FindNoteIDs(m.inferText)) > 0 {
				affected[id] = struct{}{}
			}
		}
	}
	out := make([]domain.NoteID, 0, len(affected))
	for id := range affected {
		out = append(out, id)
	}
	return out
}

// candidatesFor returns every note that could form a non-zero inferred edge with
// source m: those whose title occurs in m's text. Self is excluded.
func (idx *CorpusIndex) candidatesFor(m *noteMeta) map[domain.NoteID]struct{} {
	cands := map[domain.NoteID]struct{}{}
	if idx.matcher != nil {
		for id := range idx.matcher.FindNoteIDs(m.inferText) {
			cands[id] = struct{}{}
		}
	}
	delete(cands, m.ID)
	return cands
}
