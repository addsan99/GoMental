package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
)

func TestInferenceWorkerCoalescesBurst(t *testing.T) {
	store, err := graph.OpenSQLiteStore(graph.GraphPath(t.TempDir()))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	corpus := []domain.ParsedOKFNote{
		{ID: "alpha", Title: "Alpha", PlainText: "see beta here"},
		{ID: "beta", Title: "Beta", PlainText: "standalone"},
	}
	lc := newLiveCorpus(graph.BuildCorpusIndex(corpus))

	var mu sync.Mutex
	var graphUpdates int
	emit := func(name string, _ any) {
		if name == "graph:updated" {
			mu.Lock()
			graphUpdates++
			mu.Unlock()
		}
	}

	worker := newInferenceWorker(40*time.Millisecond, lc.Snapshot, store, emit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); worker.run(ctx) }()

	// Burst of marks well within the quiet window must coalesce to one pass.
	for i := 0; i < 6; i++ {
		worker.MarkDirty("alpha")
	}

	// Wait for the single recompute to land.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := graphUpdates
		mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no recompute within timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Give any spurious extra passes a chance to (wrongly) fire, then assert one.
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	if graphUpdates != 1 {
		mu.Unlock()
		t.Fatalf("expected exactly one coalesced pass, got %d", graphUpdates)
	}
	mu.Unlock()

	// The recompute must have written the title-mention soft link alpha->beta.
	g, err := store.FullGraph(ctx, domain.GraphFilter{IncludeSoftLinks: true})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	found := false
	for _, e := range g.Edges {
		if e.Source == "alpha" && e.Target == "beta" && e.Kind == domain.GraphEdgeInferredRelatedTo {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected inferred alpha->beta soft link, edges=%#v", g.Edges)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
