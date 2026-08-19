package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/search"
	"GoMental/internal/workspace"
)

func TestRebuildBuildsSearchGraphAndState(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\ntags: [go]\n---\n\n# Beta\n")
	var progress []RebuildProgress
	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow, Progress: func(p RebuildProgress) { progress = append(progress, p) }}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if result.TotalNotes != 2 || result.ParsedNotes != 2 || result.FailedNotes != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !hasStage(progress, ProgressComplete) {
		t.Fatalf("expected complete progress, got %#v", progress)
	}
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatalf("open search: %v", err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 || results[0].ID != "alpha" {
		t.Fatalf("unexpected search results: %#v", results)
	}
	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("unexpected backlinks: %#v", backlinks)
	}
	state, err := ReadState(result.StatePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.ParsedNotes != 2 || state.SearchPath != result.SearchPath || state.GraphPath != result.GraphPath {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestRebuildRemovesStaleSearchAndGraphEntriesAfterDelete(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n[Beta](beta.md)\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\nBeta body\n")
	rebuilder := Rebuilder{WorkerCount: 1, Now: fixedNow}
	if _, err := rebuilder.Rebuild(context.Background(), root); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "beta.md")); err != nil {
		t.Fatal(err)
	}
	result, err := rebuilder.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "Beta", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.ID == "beta" {
			t.Fatalf("stale beta search result remained: %#v", results)
		}
	}
	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 0 {
		t.Fatalf("stale beta backlink remained: %#v", backlinks)
	}
}

func TestRebuildCollapsesDuplicateMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n\n## Links\n[Beta](beta.md) and [Beta again](beta.md)\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\n# Beta\n")
	result, err := Rebuilder{WorkerCount: 1, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild duplicate links: %v", err)
	}
	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("expected duplicate markdown links to collapse, got %#v", backlinks)
	}
}
func TestRebuildContinuesAfterPartialParseFailures(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "good.md", "---\ntype: concept\ntitle: Good\n---\n\nGood body\n")
	writeNote(t, root, "bad.md", "---\ntitle: Missing type\n---\n\nBad body\n")
	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild should tolerate parse failure: %v", err)
	}
	if result.TotalNotes != 2 || result.ParsedNotes != 1 || result.FailedNotes != 1 {
		t.Fatalf("unexpected partial result: %#v", result)
	}
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "Good", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != "good" {
		t.Fatalf("good note was not indexed: %#v", results)
	}
}

func TestRebuildHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Rebuilder{WorkerCount: 1}.Rebuild(ctx, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func writeNote(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
}

func hasStage(progress []RebuildProgress, stage ProgressStage) bool {
	for _, item := range progress {
		if item.Stage == stage {
			return true
		}
	}
	return false
}

// A workspace's ingest profile must reach the parser through the rebuild path
// too. Rebuild opens the workspace itself, so it cannot rely on the application
// service having installed the profile first: forgetting that leaves scanning
// profile-aware while parsing is not, and every note fails to parse.
func TestRebuildAppliesWorkspaceIngestProfile(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, filepath.Join("topics", "alpha.md"), "---\ntitle: Alpha\nkeywords: [pop]\ndepth: hub\ndepends_on: [beta.md]\n---\n\n# Alpha\n")
	writeNote(t, root, filepath.Join("topics", "beta.md"), "---\ntitle: Beta\ndepth: detail\n---\n\n# Beta\n")
	writeNote(t, root, filepath.Join("topics", "skipped", "gamma.md"), "---\ntitle: Gamma\n---\n\n# Gamma\n")
	if err := os.MkdirAll(filepath.Join(root, ".gomental"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := "version: 1\n" +
		"rules:\n" +
		"  - match: \"topics/**\"\n" +
		"    defaults:\n" +
		"      type: topic\n" +
		"    searchAliases: [keywords]\n" +
		"    tagFields: [depth]\n" +
		"    links:\n" +
		"      - field: depends_on\n" +
		"        basePath: topics\n" +
		"exclude:\n" +
		"  - \"topics/skipped/**\"\n"
	if err := os.WriteFile(filepath.Join(root, ".gomental", "mapping.yaml"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	// None of these notes declare a type, so without the profile every one of
	// them fails to parse and the excluded note is still scanned.
	if result.TotalNotes != 2 || result.ParsedNotes != 2 || result.FailedNotes != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}

	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer store.Close()
	backlinks, err := store.Backlinks(context.Background(), "topics/beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "topics/alpha" {
		t.Fatalf("frontmatter link did not reach the graph: %#v", backlinks)
	}

	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatalf("open search: %v", err)
	}
	defer idx.Close()
	results, err := idx.Search(context.Background(), domain.SearchQuery{Text: "pop", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "topics/alpha" {
		t.Fatalf("keyword alias is not searchable: %#v", results)
	}
	tagged, err := idx.Search(context.Background(), domain.SearchQuery{Text: "alpha", Tags: []domain.Tag{"hub"}, Limit: 10})
	if err != nil {
		t.Fatalf("search by tag: %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != "topics/alpha" {
		t.Fatalf("mapped tag is not filterable: %#v", tagged)
	}
}

// A composite has to keep each member's wiki-links pointing inside that member,
// even when both members use the same note names, while still letting shared
// tags join them into one graph.
func TestRebuildComposite(t *testing.T) {
	base := t.TempDir()
	alpha := filepath.Join(base, "alpha")
	beta := filepath.Join(base, "beta")
	root := filepath.Join(base, "all")
	for _, dir := range []string{alpha, beta, root} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Both members contain a "shared" note, and both link to it by bare name.
	writeNote(t, alpha, "entry.md", "---\ntype: concept\ntitle: Alpha entry\ntags: [common]\n---\n\n# Alpha entry\nSee [[shared]].\n")
	writeNote(t, alpha, "shared.md", "---\ntype: concept\ntitle: Alpha shared\ntags: [common]\n---\n\n# Alpha shared\n")
	writeNote(t, beta, "entry.md", "---\ntype: concept\ntitle: Beta entry\ntags: [common]\n---\n\n# Beta entry\nSee [[shared]].\n")
	writeNote(t, beta, "shared.md", "---\ntype: concept\ntitle: Beta shared\ntags: [common]\n---\n\n# Beta shared\n")

	if err := workspace.WriteCompositeConfig(root, []string{alpha, beta}); err != nil {
		t.Fatal(err)
	}

	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatalf("rebuild composite: %v", err)
	}
	if result.TotalNotes != 4 || result.ParsedNotes != 4 {
		t.Fatalf("expected all 4 member notes, got %#v", result)
	}

	store, err := graph.OpenSQLiteStore(result.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Each member's link must land on its own shared note, not the other's.
	for _, member := range []string{"alpha", "beta"} {
		backlinks, err := store.Backlinks(context.Background(), domain.NoteID(member+"/shared"))
		if err != nil {
			t.Fatal(err)
		}
		if len(backlinks) != 1 || backlinks[0].Source != domain.NoteID(member+"/entry") {
			t.Fatalf("%s/shared has backlinks %#v; expected only %s/entry", member, backlinks, member)
		}
	}

	// Wiki-links cannot cross members, so what carries the composite is the
	// shared metadata hubs: a tag both members use must gather both members.
	joined, err := store.Query(context.Background(), domain.GraphQuery{
		MetadataSeed:         "tag:common",
		Depth:                1,
		IncludeMetadataLinks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	reached := map[string]bool{}
	for _, node := range joined.Nodes {
		if node.NoteID != nil {
			reached[string(*node.NoteID)] = true
		}
	}
	if !reached["beta/entry"] {
		t.Fatalf("expected the shared tag to connect members; reached %v", reached)
	}

	// Search spans members and reports namespaced ids.
	idx, err := search.OpenBleveIndex(result.SearchPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	hits, err := idx.Search(context.Background(), domain.SearchQuery{Text: "shared", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, hit := range hits {
		found[string(hit.ID)] = true
	}
	if !found["alpha/shared"] || !found["beta/shared"] {
		t.Fatalf("expected both members' notes in search, got %v", found)
	}
}

// An ingest profile matches notes by path, and those paths are relative to the
// member that owns the profile. Decoding under the composite's namespaced id
// would silently stop matching every rule, which shows up as notes that "have no
// frontmatter" even though they parse fine on their own.
func TestRebuildCompositeAppliesMemberRelativeIngestProfiles(t *testing.T) {
	base := t.TempDir()
	profiled := filepath.Join(base, "profiled")
	plain := filepath.Join(base, "plain")
	root := filepath.Join(base, "all")
	for _, dir := range []string{profiled, plain, root} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNote(t, profiled, filepath.Join(".gomental", "mapping.yaml"), "version: 1\nrules:\n  - match: topics/**\n    defaults:\n      type: topic\n")
	// No frontmatter: only the profile's default type makes this note parseable.
	writeNote(t, profiled, "topics/alpha.md", "# Alpha\nBody.\n")
	writeNote(t, plain, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\n# Beta\n")

	if err := workspace.WriteCompositeConfig(root, []string{profiled, plain}); err != nil {
		t.Fatal(err)
	}
	result, err := Rebuilder{WorkerCount: 2, Now: fixedNow}.Rebuild(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if result.FailedNotes != 0 || result.ParsedNotes != 2 {
		t.Fatalf("member ingest profile did not apply: %#v", result)
	}
}
