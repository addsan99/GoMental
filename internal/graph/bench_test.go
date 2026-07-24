package graph

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"GoMental/internal/domain"
)

var benchTags = []domain.Tag{"go", "db", "api", "ui", "perf", "docs", "test", "net", "auth", "cli", "sql", "web", "cache", "queue", "log", "cfg", "graph", "index", "sync", "mcp"}
var benchTypes = []string{"concept", "reference", "howto", "note", "spec"}
var benchHeadings = []string{"Overview", "Design", "Usage", "Notes", "API", "Examples", "Caveats", "History"}
var benchWords = []string{"the", "quick", "brown", "fox", "data", "node", "link", "graph", "note", "system", "value", "store", "index", "query", "cache", "worker", "signal", "buffer", "commit", "resolve"}

var benchTitleWords = []string{
	"project", "design", "auth", "billing", "cache", "graph", "search", "index",
	"queue", "worker", "schema", "migration", "webhook", "session", "token", "policy",
	"invoice", "tenant", "agent", "sync", "ledger", "vault", "router", "sched",
	"metrics", "trace", "alert", "digest", "sandbox", "runtime", "kernel", "shard",
}

// genCorpus builds n synthetic notes with realistic multi-word titles (drawn from a
// word pool, so prefix-sharing is modest — unlike a "Title-000001" scheme that makes
// a degenerate trie). Each body carries ~120 filler words plus 3 verbatim title
// mentions, so realistic title-mention soft links form (~1 KB bodies).
func genCorpus(n int, seed int64) []domain.ParsedOKFNote {
	rng := rand.New(rand.NewSource(seed))
	titles := make([]string, n)
	seen := map[string]struct{}{}
	for i := range titles {
		for {
			t := fmt.Sprintf("%s %s %d",
				benchTitleWords[rng.Intn(len(benchTitleWords))],
				benchTitleWords[rng.Intn(len(benchTitleWords))],
				rng.Intn(n))
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			titles[i] = t
			break
		}
	}
	notes := make([]domain.ParsedOKFNote, n)
	for i := 0; i < n; i++ {
		var sb strings.Builder
		for w := 0; w < 120; w++ {
			sb.WriteString(benchWords[rng.Intn(len(benchWords))])
			sb.WriteByte(' ')
		}
		for m := 0; m < 3; m++ {
			sb.WriteString(titles[rng.Intn(n)])
			sb.WriteByte(' ')
		}
		tags := make([]domain.Tag, 0, 3)
		for t := 0; t < 2+rng.Intn(2); t++ {
			tags = append(tags, benchTags[rng.Intn(len(benchTags))])
		}
		heads := make([]domain.Heading, 0, 3)
		for h := 0; h < 1+rng.Intn(3); h++ {
			heads = append(heads, domain.Heading{Level: 1, Text: benchHeadings[rng.Intn(len(benchHeadings))]})
		}
		notes[i] = domain.ParsedOKFNote{
			ID:        domain.NoteID(fmt.Sprintf("n%06d", i)),
			Title:     titles[i],
			PlainText: sb.String(),
			Metadata:  domain.OKFMetadata{Type: benchTypes[rng.Intn(len(benchTypes))]},
			Tags:      tags,
			Headings:  heads,
		}
	}
	return notes
}

func BenchmarkInferAll(b *testing.B) {
	svc := NewLocalInferenceService(InferenceConfig{})
	for _, n := range []int{1000, 5000, 10000} {
		corpus := genCorpus(n, 1)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				idx := BuildCorpusIndex(corpus)
				if _, err := svc.InferAll(context.Background(), idx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTitleMatcherScan measures the throughput (SetBytes -> MB/s) of scanning
// every note body through the full-corpus title matcher. This is the number that
// decides whether the map-based automaton needs optimizing.
func BenchmarkTitleMatcherScan(b *testing.B) {
	corpus := genCorpus(10000, 1)
	patterns := map[string][]domain.NoteID{}
	texts := make([]string, len(corpus))
	var total int64
	for i, note := range corpus {
		patterns[strings.ToLower(note.Title)] = append(patterns[strings.ToLower(note.Title)], note.ID)
		texts[i] = strings.ToLower(note.PlainText)
		total += int64(len(texts[i]))
	}
	m := newTitleMatcher(patterns)
	b.SetBytes(total)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, t := range texts {
			_ = m.FindNoteIDs(t)
		}
	}
}

// BenchmarkIncrementalVsFull compares a full InferAll against the Phase-B minimal
// affected-set recompute for a single changed note on a 10k corpus.
func BenchmarkIncrementalVsFull(b *testing.B) {
	svc := NewLocalInferenceService(InferenceConfig{})
	corpus := genCorpus(10000, 1)
	prev := BuildCorpusIndex(corpus)

	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cur := BuildCorpusIndex(corpus)
			if _, err := svc.InferAll(context.Background(), cur); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("incremental-1-changed", func(b *testing.B) {
		b.ReportAllocs()
		changed := []domain.NoteID{corpus[0].ID}
		for i := 0; i < b.N; i++ {
			cur := BuildCorpusIndex(corpus)
			affected := ComputeAffectedSources(cur, prev, changed, nil)
			for _, id := range affected {
				if _, err := svc.InferOne(context.Background(), cur, id); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func BenchmarkNeighborhoodCTE(b *testing.B) {
	corpus := genCorpus(10000, 1)
	store, err := OpenSQLiteStore(GraphPath(b.TempDir()))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	idx := BuildCorpusIndex(corpus)
	all, err := NewLocalInferenceService(InferenceConfig{}).InferAll(context.Background(), idx)
	if err != nil {
		b.Fatal(err)
	}
	projections := make([]LinkProjection, 0, len(all))
	for id, soft := range all {
		if len(soft) > 0 {
			projections = append(projections, LinkProjection{Source: id, Soft: soft})
		}
	}
	if err := store.ReplaceAllInferredLinks(context.Background(), projections); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for _, depth := range []int{1, 2} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := store.Neighborhood(context.Background(), corpus[i%len(corpus)].ID, depth); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFullGraphMetadata(b *testing.B) {
	corpus := genCorpus(10000, 1)
	store, err := OpenSQLiteStore(GraphPath(b.TempDir()))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	for _, note := range corpus {
		if err := store.ReplaceMetadataLinks(context.Background(), note.ID, MetadataMemberships(note)); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g, err := store.FullGraph(context.Background(), domain.GraphFilter{IncludeMetadataLinks: true})
		if err != nil {
			b.Fatal(err)
		}
		if i == 0 {
			b.ReportMetric(float64(len(g.Edges)), "edges")
			b.ReportMetric(float64(len(g.Nodes)), "nodes")
		}
	}
}
