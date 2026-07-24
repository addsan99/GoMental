package graph

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"GoMental/internal/domain"
)

// TestAffectedSetEqualsFullRebuild is the correctness guard for incremental
// recompute: applying a random (changed, deleted) batch and recomputing only the
// affected sources must yield the exact same soft-link set as a from-scratch
// InferAll over the mutated corpus.
func TestAffectedSetEqualsFullRebuild(t *testing.T) {
	titles := []string{"Alpha", "Beta", "Gamma", "Ga", "Delta", "Overview", ""}
	// Fixed clock so ComputedAt is deterministic across the two InferAll passes.
	fixed := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	svc := NewLocalInferenceService(InferenceConfig{Now: func() time.Time { return fixed }})
	ctx := context.Background()

	makeNote := func(rng *rand.Rand, id domain.NoteID) domain.ParsedOKFNote {
		title := titles[rng.Intn(len(titles))]
		// Body mentions a random handful of titles so soft links actually form.
		body := ""
		for k := 0; k < rng.Intn(4); k++ {
			body += " " + titles[rng.Intn(len(titles))]
		}
		return domain.ParsedOKFNote{ID: id, Title: title, PlainText: body}
	}

	normalize := func(m map[domain.NoteID][]domain.InferredNoteLink) map[domain.NoteID][]domain.InferredNoteLink {
		out := map[domain.NoteID][]domain.InferredNoteLink{}
		for id, links := range m {
			if len(links) > 0 {
				out[id] = links
			}
		}
		return out
	}

	for seed := int64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		n := 3 + rng.Intn(8)
		prevNotes := make([]domain.ParsedOKFNote, n)
		for i := 0; i < n; i++ {
			prevNotes[i] = makeNote(rng, domain.NoteID(fmt.Sprintf("n%d", i)))
		}
		prev := BuildCorpusIndex(prevNotes)
		prevAll, _ := svc.InferAll(ctx, prev)
		// incremental state we will maintain
		state := map[domain.NoteID][]domain.InferredNoteLink{}
		for id, links := range prevAll {
			state[id] = links
		}

		// Build the mutated corpus and the changed/deleted deltas.
		var changed, deleted []domain.NoteID
		curNotes := make([]domain.ParsedOKFNote, 0, n+2)
		for _, note := range prevNotes {
			switch rng.Intn(5) {
			case 0: // delete
				deleted = append(deleted, note.ID)
			case 1, 2: // change
				changed = append(changed, note.ID)
				curNotes = append(curNotes, makeNote(rng, note.ID))
			default: // keep
				curNotes = append(curNotes, note)
			}
		}
		// occasionally add new notes
		for k := 0; k < rng.Intn(3); k++ {
			id := domain.NoteID(fmt.Sprintf("new%d", k))
			changed = append(changed, id)
			curNotes = append(curNotes, makeNote(rng, id))
		}

		cur := BuildCorpusIndex(curNotes)

		// Apply the incremental recompute.
		for _, id := range deleted {
			delete(state, id)
		}
		affected := ComputeAffectedSources(cur, prev, changed, deleted)
		for _, id := range affected {
			links, _ := svc.InferOne(ctx, cur, id)
			if len(links) > 0 {
				state[id] = links
			} else {
				delete(state, id)
			}
		}
		// Sources that no longer exist must not linger.
		for id := range state {
			if _, ok := cur.byID[id]; !ok {
				delete(state, id)
			}
		}

		full, _ := svc.InferAll(ctx, cur)
		if !reflect.DeepEqual(normalize(state), normalize(full)) {
			t.Fatalf("seed %d: incremental != full\nchanged=%v deleted=%v\n incr=%#v\n full=%#v",
				seed, changed, deleted, normalize(state), normalize(full))
		}
	}
}
