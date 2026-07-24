package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/workspace"
)

func testBenchService(b *testing.B) *Service {
	b.Helper()
	store := workspace.NewRecentWorkspaceStore(filepath.Join(b.TempDir(), "recent.json"), 10)
	svc := NewServiceWithStores(nil, store, filepath.Join(b.TempDir(), "ui-state.json"))
	b.Cleanup(func() { _ = svc.Close() })
	return svc
}

func writeBenchWorkspace(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("---\ntype: concept\ntitle: Title-%06d-end\ntags: [go, db]\n---\n\n# Title-%06d-end\nBody mentions Title-%06d-end.\n", i, i, (i+1)%n)
		path := filepath.Join(root, fmt.Sprintf("n%06d.md", i))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

// BenchmarkResolverIDSource is the direct before/after for the save hot path's link
// resolver: the removed repo.List (filesystem WalkDir + stat over the whole tree)
// vs the in-memory liveCorpus.ResolverIDs it was replaced with.
func BenchmarkResolverIDSource(b *testing.B) {
	for _, n := range []int{1000, 5000, 10000} {
		root := writeBenchWorkspace(b, n)
		ws, err := workspace.Open(root)
		if err != nil {
			b.Fatal(err)
		}
		repo := workspace.NewFileNoteRepository(ws)
		ctx := context.Background()
		summaries, err := repo.List(ctx)
		if err != nil {
			b.Fatal(err)
		}
		parsed := make([]domain.ParsedOKFNote, len(summaries))
		for i, s := range summaries {
			parsed[i] = domain.ParsedOKFNote{ID: s.ID}
		}
		lc := newLiveCorpus(graph.BuildCorpusIndex(parsed))

		b.Run(fmt.Sprintf("before_repoList_WalkDir/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := repo.List(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("after_liveCorpus_ResolverIDs/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = lc.ResolverIDs()
			}
		})
	}
}

// BenchmarkSaveNoteHotPath measures end-to-end synchronous save latency at several
// corpus sizes; it should be roughly flat (independent of n) now that the tree walk
// and inference are off the hot path.
func BenchmarkSaveNoteHotPath(b *testing.B) {
	for _, n := range []int{1000, 5000, 10000} {
		root := writeBenchWorkspace(b, n)
		svc := testBenchService(b)
		ctx := context.Background()
		if _, err := svc.OpenWorkspace(ctx, root); err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				content := fmt.Sprintf("---\ntype: concept\ntitle: Title-000000-end\n---\n\n# Title-000000-end\nedit %d mentions Title-000001-end.\n", i)
				if _, err := svc.SaveNote(ctx, SaveNoteRequest{ID: "n000000", Content: content}); err != nil {
					b.Fatal(err)
				}
			}
		})
		_ = svc.Close()
	}
}
