package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/indexing"
)

// inferenceWorker recomputes soft (inferred) links off the hot path. One worker
// lives per open workspace. Edits enqueue dirty/deleted note IDs and wake the
// worker; after a quiet period it drains the queue and recomputes, coalescing a
// burst of edits into a single pass. Unlike the old restart-per-edit scheme it
// never cancels a running pass on a new edit — new edits accumulate into the next
// drain — and it is cancelled only when the workspace closes.
//
// Phase D recomputes the WHOLE corpus each pass (correct and coalesced; the eventual
// soft-link set is identical to a from-scratch rebuild). The drained dirty/deleted
// sets are tracked now so Phase B can switch to a minimal affected-set recompute
// without changing the wiring.
type inferenceWorker struct {
	quiet    time.Duration
	snapshot func() *graph.CorpusIndex
	store    *graph.SQLiteStore
	emit     func(string, any)

	mu      sync.Mutex
	dirty   map[domain.NoteID]struct{}
	deleted map[domain.NoteID]struct{}
	pending bool
	wake    chan struct{}

	// lastSnap is the corpus state as of the previous recompute, used to diff old
	// vs new metadata for the incremental affected-set. Accessed only from run().
	lastSnap *graph.CorpusIndex
}

func newInferenceWorker(quiet time.Duration, snapshot func() *graph.CorpusIndex, store *graph.SQLiteStore, emit func(string, any)) *inferenceWorker {
	return &inferenceWorker{
		quiet:    quiet,
		snapshot: snapshot,
		store:    store,
		emit:     emit,
		dirty:    map[domain.NoteID]struct{}{},
		deleted:  map[domain.NoteID]struct{}{},
		wake:     make(chan struct{}, 1),
	}
}

// MarkDirty enqueues notes whose content changed (empty call still schedules a
// pass, e.g. an initial full rebuild) and wakes the worker.
func (w *inferenceWorker) MarkDirty(ids ...domain.NoteID) {
	w.mu.Lock()
	for _, id := range ids {
		w.dirty[id] = struct{}{}
	}
	w.pending = true
	w.mu.Unlock()
	w.signal()
}

// MarkDeleted enqueues deleted notes and wakes the worker.
func (w *inferenceWorker) MarkDeleted(ids ...domain.NoteID) {
	w.mu.Lock()
	for _, id := range ids {
		w.deleted[id] = struct{}{}
	}
	w.pending = true
	w.mu.Unlock()
	w.signal()
}

func (w *inferenceWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// run is the worker loop. It exits when ctx is cancelled (workspace close).
func (w *inferenceWorker) run(ctx context.Context) {
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-w.wake:
			if timer == nil {
				timer = time.NewTimer(w.quiet)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.quiet)
			}
		case <-timerC:
			timer = nil
			timerC = nil
			w.recompute(ctx)
		}
	}
}

// recompute drains the queue and recomputes soft links. The first pass (no prior
// snapshot) recomputes the whole corpus; subsequent passes recompute only the
// minimal affected-source set, which is provably identical to a full rebuild.
func (w *inferenceWorker) recompute(ctx context.Context) {
	w.mu.Lock()
	changed := keysOf(w.dirty)
	deleted := keysOf(w.deleted)
	w.dirty = map[domain.NoteID]struct{}{}
	w.deleted = map[domain.NoteID]struct{}{}
	w.pending = false
	w.mu.Unlock()

	cur := w.snapshot()
	if cur == nil {
		return
	}
	w.emit("index:progress", indexing.RebuildProgress{Stage: indexing.ProgressGraph, Message: "Rebuilding inferred links"})
	inference := graph.NewLocalInferenceService(graph.InferenceConfig{})

	if w.lastSnap == nil {
		all, err := inference.InferAll(ctx, cur)
		if err != nil {
			w.reportErr(err)
			return
		}
		projections := make([]graph.LinkProjection, 0, len(all))
		for id, soft := range all {
			if len(soft) == 0 {
				continue
			}
			projections = append(projections, graph.LinkProjection{Source: id, Soft: soft})
		}
		if err := w.store.ReplaceAllInferredLinks(ctx, projections); err != nil {
			w.reportErr(err)
			return
		}
	} else {
		affected := graph.ComputeAffectedSources(cur, w.lastSnap, changed, deleted)
		for _, id := range affected {
			if err := ctx.Err(); err != nil {
				return
			}
			soft, err := inference.InferOne(ctx, cur, id)
			if err != nil {
				w.reportErr(err)
				return
			}
			if err := w.store.ReplaceInferredLinks(ctx, id, soft); err != nil {
				w.reportErr(err)
				return
			}
		}
	}
	w.lastSnap = cur
	w.emit("index:progress", indexing.RebuildProgress{Stage: indexing.ProgressComplete, Message: "Inferred links complete"})
	w.emit("graph:updated", map[string]string{"kind": "soft-links"})
}

func (w *inferenceWorker) reportErr(err error) {
	if !errors.Is(err, context.Canceled) {
		w.emit("index:progress", indexing.RebuildProgress{Stage: indexing.ProgressGraph, Message: err.Error()})
	}
}

func keysOf(m map[domain.NoteID]struct{}) []domain.NoteID {
	out := make([]domain.NoteID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}
