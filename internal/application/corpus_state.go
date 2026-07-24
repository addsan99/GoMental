package application

import (
	"sync"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
)

// liveCorpus is the Service's in-memory, concurrency-safe note index. It lets the
// save hot path build a link resolver from an in-memory ID set instead of walking
// the workspace tree, and lets the background inference worker snapshot the corpus
// without reparsing files. Its lifetime tracks an open workspace (swapped under the
// Service mutex on open/close); reads/writes to the wrapped index are guarded here.
type liveCorpus struct {
	mu  sync.RWMutex
	idx *graph.CorpusIndex
}

func newLiveCorpus(idx *graph.CorpusIndex) *liveCorpus {
	return &liveCorpus{idx: idx}
}

// ResolverIDs returns the current note ID set for okf.NewResolver.
func (c *liveCorpus) ResolverIDs() []domain.NoteID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.idx == nil {
		return nil
	}
	return c.idx.ResolverIDs()
}

// Upsert adds or replaces a note (incremental; used on the save/create hot path).
func (c *liveCorpus) Upsert(note domain.ParsedOKFNote) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx == nil {
		return
	}
	c.idx.Upsert(note)
}

// Delete removes a note.
func (c *liveCorpus) Delete(id domain.NoteID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx == nil {
		return
	}
	c.idx.Delete(id)
}

// Replace swaps the whole index (used after a watcher batch reparses the corpus,
// keeping the in-memory ID set authoritative against external edits).
func (c *liveCorpus) Replace(idx *graph.CorpusIndex) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.idx = idx
}

// Snapshot returns an isolated copy safe to read while saves continue.
func (c *liveCorpus) Snapshot() *graph.CorpusIndex {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.idx == nil {
		return nil
	}
	return c.idx.Clone()
}
