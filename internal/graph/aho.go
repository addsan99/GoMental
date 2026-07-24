package graph

import (
	"strings"

	"GoMental/internal/domain"
)

// titleMatcher is an Aho-Corasick multi-substring matcher over lowercased title
// bytes. It reports a match exactly when a pattern is a byte-substring of the
// scanned text — identical semantics to running strings.Contains(text, pattern)
// for every pattern, which is what inferEvidence uses for title mentions. Working
// on bytes (not runes) mirrors strings.Contains over UTF-8 exactly.
type titleMatcher struct {
	children []map[byte]int
	fail     []int
	outputs  [][]domain.NoteID // note IDs whose title terminates at this node (fail-chain merged)
}

// newTitleMatcher builds the automaton from lowercased, non-empty title patterns
// mapped to the note IDs carrying that title (a title may be shared by several
// notes). Empty patterns and empty ID lists are ignored.
func newTitleMatcher(patterns map[string][]domain.NoteID) *titleMatcher {
	m := &titleMatcher{
		children: []map[byte]int{{}}, // node 0 = root
		fail:     []int{0},
		outputs:  [][]domain.NoteID{nil},
	}
	for pat, ids := range patterns {
		if pat == "" || len(ids) == 0 {
			continue
		}
		cur := 0
		for i := 0; i < len(pat); i++ {
			b := pat[i]
			next, ok := m.children[cur][b]
			if !ok {
				next = len(m.children)
				m.children = append(m.children, map[byte]int{})
				m.fail = append(m.fail, 0)
				m.outputs = append(m.outputs, nil)
				m.children[cur][b] = next
			}
			cur = next
		}
		m.outputs[cur] = append(m.outputs[cur], ids...)
	}
	m.buildFailLinks()
	return m
}

func (m *titleMatcher) buildFailLinks() {
	queue := make([]int, 0, len(m.children))
	for _, next := range m.children[0] {
		m.fail[next] = 0
		queue = append(queue, next)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for b, next := range m.children[cur] {
			f := m.fail[cur]
			for f != 0 {
				if _, ok := m.children[f][b]; ok {
					break
				}
				f = m.fail[f]
			}
			if nf, ok := m.children[f][b]; ok && nf != next {
				m.fail[next] = nf
			} else {
				m.fail[next] = 0
			}
			// The fail node is shallower and was processed earlier in BFS order, so
			// its outputs are already fully merged.
			m.outputs[next] = append(m.outputs[next], m.outputs[m.fail[next]]...)
			queue = append(queue, next)
		}
	}
}

// FindNoteIDs returns the set of note IDs whose title occurs as a substring of
// text. text must already be lowercased (the caller passes inferText).
func (m *titleMatcher) FindNoteIDs(text string) map[domain.NoteID]struct{} {
	result := map[domain.NoteID]struct{}{}
	cur := 0
	for i := 0; i < len(text); i++ {
		b := text[i]
		for cur != 0 {
			if _, ok := m.children[cur][b]; ok {
				break
			}
			cur = m.fail[cur]
		}
		if next, ok := m.children[cur][b]; ok {
			cur = next
		} else {
			cur = 0
		}
		for _, id := range m.outputs[cur] {
			result[id] = struct{}{}
		}
	}
	return result
}

// bruteForceFindNoteIDs is the O(patterns*len) reference implementation the
// automaton must agree with. Kept beside newTitleMatcher as a correctness oracle
// (used by tests) and as a documented, obviously-correct fallback.
func bruteForceFindNoteIDs(patterns map[string][]domain.NoteID, text string) map[domain.NoteID]struct{} {
	result := map[domain.NoteID]struct{}{}
	for pat, ids := range patterns {
		if pat == "" {
			continue
		}
		if strings.Contains(text, pat) {
			for _, id := range ids {
				result[id] = struct{}{}
			}
		}
	}
	return result
}
