package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/indexing"
	"GoMental/internal/platform"
	"GoMental/internal/search"
	"GoMental/internal/workspace"
)

func TestServiceExplainLinkAndExpandContext(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\ntags: [go]\n---\n\n# Beta\nBeta body content.\n")
	service := testService(t, func(string, any) {})
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	expl, err := service.ExplainLink(ctx, "alpha", "beta")
	if err != nil {
		t.Fatalf("explain link: %v", err)
	}
	if !expl.Related || !expl.HardLink {
		t.Fatalf("expected related hard link: %#v", expl)
	}
	if expl.Score <= 0 || expl.Summary == "" {
		t.Fatalf("expected score and summary: %#v", expl)
	}
	var hasTag, hasMention bool
	for _, ev := range expl.Evidence {
		switch ev.Kind {
		case string(domain.EvidenceSharedTag):
			hasTag = true
		case string(domain.EvidenceTitleMention):
			hasMention = true
		}
	}
	if !hasTag || !hasMention {
		t.Fatalf("expected shared_tag + title_mention evidence: %#v", expl.Evidence)
	}

	dto, err := service.ExpandContext(ctx, "alpha", 1)
	if err != nil {
		t.Fatalf("expand context: %v", err)
	}
	if dto.ID != "alpha" || dto.Content == "" || dto.Version == "" {
		t.Fatalf("unexpected focus: %#v", dto)
	}
	found := false
	for _, n := range dto.Neighbors {
		if n.ID == "beta" {
			found = true
			if n.Excerpt == "" {
				t.Fatalf("expected beta excerpt: %#v", n)
			}
		}
	}
	if !found {
		t.Fatalf("expected beta neighbor: %#v", dto.Neighbors)
	}
}

func TestServiceWorkspaceNotesSearchGraphAndRebuild(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\ntags: [go]\n---\n\n# Beta\n")
	var events []string
	service := testService(t, func(name string, payload any) { events = append(events, name) })
	ctx := context.Background()

	opened, err := service.OpenWorkspace(ctx, root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if opened.NoteCount != 2 {
		t.Fatalf("unexpected workspace dto: %#v", opened)
	}
	rebuild, err := service.Rebuild(ctx)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuild.ParsedNotes != 2 || rebuild.FailedNotes != 0 {
		t.Fatalf("unexpected rebuild: %#v", rebuild)
	}
	if !contains(events, "workspace:loaded") || !contains(events, "index:progress") || !contains(events, "graph:updated") {
		t.Fatalf("expected key events, got %#v", events)
	}
	notes, err := service.ListNotes(ctx)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("unexpected notes: %#v", notes)
	}
	note, err := service.ReadNote(ctx, "alpha")
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if note.ID != "alpha" || note.Content == "" {
		t.Fatalf("unexpected note: %#v", note)
	}
	results, err := service.Search(ctx, SearchQueryDTO{Text: "Alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 || results[0].ID != "alpha" {
		t.Fatalf("unexpected search results: %#v", results)
	}
	backlinks, err := service.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("unexpected backlinks: %#v", backlinks)
	}
	full, err := service.FullGraph(ctx, GraphFilterDTO{IncludeUnresolved: true, IncludeSoftLinks: true})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if len(full.Nodes) == 0 || len(full.Edges) == 0 {
		t.Fatalf("expected graph data, got %#v", full)
	}
}

func TestServiceListNotesCarriesTypeAndFilters(t *testing.T) {
	root := t.TempDir()
	// Two different type values prove filtering is taxonomy-agnostic (no hardcoded types).
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\n")
	writeNote(t, root, "beta.md", "---\ntype: procedure\ntitle: Beta\ntags: [go]\n---\n\n# Beta\n")
	service := testService(t, func(string, any) {})
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// Every listed note carries its type.
	notes, err := service.ListNotes(ctx)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	types := map[string]string{}
	for _, n := range notes {
		types[n.ID] = n.Type
	}
	if types["alpha"] != "concept" || types["beta"] != "procedure" {
		t.Fatalf("expected types populated, got %#v", types)
	}

	// The paginated query filters by type (exact, case-insensitive).
	page, err := service.ListNotesPage(ctx, ListNotesQueryDTO{Type: "PROCEDURE"})
	if err != nil {
		t.Fatalf("list notes page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "beta" || page.Items[0].Type != "procedure" {
		t.Fatalf("expected only beta for type=procedure, got %#v", page)
	}
}

func TestSetNoteFavoriteToleratesInvalidOKFMetadata(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "missing type",
			content: "---\ntitle: Loose Note\n---\n\n# Loose Note\n",
		},
		{
			name:    "malformed yaml",
			content: "---\ntitle: [broken\n---\n\n# Broken YAML\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeNote(t, root, "loose.md", tc.content)
			service := testService(t, nil)
			ctx := context.Background()
			if _, err := service.OpenWorkspace(ctx, root); err != nil {
				t.Fatalf("open workspace: %v", err)
			}

			favorited, err := service.SetNoteFavorite(ctx, "loose", true)
			if err != nil {
				t.Fatalf("favorite invalid note: %v", err)
			}
			if !favorited.Favorite || !strings.Contains(favorited.Content, "favorite: true") {
				t.Fatalf("expected favorite true in dto/content, got %#v", favorited)
			}
			notes, err := service.ListNotes(ctx)
			if err != nil {
				t.Fatalf("list notes: %v", err)
			}
			if len(notes) != 1 || !notes[0].Favorite {
				t.Fatalf("expected favorite in note list, got %#v", notes)
			}

			unfavorited, err := service.SetNoteFavorite(ctx, "loose", false)
			if err != nil {
				t.Fatalf("unfavorite invalid note: %v", err)
			}
			if unfavorited.Favorite || strings.Contains(unfavorited.Content, "favorite: true") {
				t.Fatalf("expected favorite removed, got %#v", unfavorited)
			}
		})
	}
}

func TestSetNoteFavoriteRepairsProjectionWhenFileAlreadyMatches(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\nfavorite: true\n---\n\n# Alpha\n")
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	if err := service.graphStore.UpsertNoteMeta(ctx, graph.NoteMeta{
		ID:         "alpha",
		Title:      "alpha",
		Path:       "alpha.md",
		Type:       "concept",
		ModifiedAt: time.Now(),
		Favorite:   false,
	}); err != nil {
		t.Fatalf("stale graph favorite: %v", err)
	}
	if err := service.searchIndex.Index(ctx, domain.SearchDocument{
		ID:       "alpha",
		Path:     "alpha.md",
		Title:    "Alpha",
		Body:     "Alpha",
		Favorite: false,
	}); err != nil {
		t.Fatalf("stale search favorite: %v", err)
	}

	favorited, err := service.SetNoteFavorite(ctx, "alpha", true)
	if err != nil {
		t.Fatalf("favorite alpha: %v", err)
	}
	if !favorited.Favorite {
		t.Fatalf("expected favorite dto, got %#v", favorited)
	}
	notes, err := service.ListNotes(ctx)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 || !notes[0].Favorite {
		t.Fatalf("expected repaired favorite in note list, got %#v", notes)
	}
	results, err := service.Search(ctx, SearchQueryDTO{Text: "Alpha", FavoritesOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("favorite search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "alpha" || !results[0].Favorite {
		t.Fatalf("expected repaired favorite search hit, got %#v", results)
	}
}

func TestServiceRebuildClosesOpenProjectionFiles(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n")
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Search(ctx, SearchQueryDTO{Text: "Alpha", Limit: 10}); err != nil {
		t.Fatalf("search before rebuild: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild with open projection files: %v", err)
	}
	assertSearchHit(t, service, "Alpha", "alpha")
}

func TestServiceSavesAndLoadsNoteImageAssets(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "folder/alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\n")
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	asset, err := service.SaveNoteAsset(ctx, SaveNoteAssetRequest{
		NoteID:     "folder/alpha",
		FileName:   "Pasted Image.png",
		MIMEType:   "image/png",
		DataBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatalf("save note asset: %v", err)
	}
	if asset.Path == "" || asset.Markdown == "" {
		t.Fatalf("expected asset response, got %#v", asset)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "folder", "alpha")); err != nil {
		t.Fatalf("expected note asset folder: %v", err)
	}
	dataURL, err := service.LoadNoteAssetDataURL(ctx, NoteAssetRequest{NoteID: "folder/alpha", Path: asset.Path})
	if err != nil {
		t.Fatalf("load note asset: %v", err)
	}
	if want := "data:image/png;base64,"; len(dataURL) < len(want) || dataURL[:len(want)] != want {
		t.Fatalf("unexpected data URL: %q", dataURL)
	}
	if _, err := service.LoadNoteAssetDataURL(ctx, NoteAssetRequest{NoteID: "folder/alpha", Path: "../../outside.png"}); err == nil {
		t.Fatal("expected path traversal to fail")
	}
}

func TestServiceMoveNoteRewritesLocalImageLinks(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "assets", "recipes", "pargit_marinade")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("create asset dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "image.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "photo.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write html asset: %v", err)
	}
	content := "---\ntype: recipe\ntitle: Pargit Marinade\n---\n\n# Pargit Marinade\n\n![ingredients](../assets/recipes/pargit_marinade/image.png)\n<img src=\"../assets/recipes/pargit_marinade/photo.png\">\n![remote](https://example.com/remote.png)\n"
	writeNote(t, root, "recipes/pargit_marinade.md", content)
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	moved, err := service.MoveNote(ctx, MoveNoteRequest{ID: "recipes/pargit_marinade", NewID: "pargit_marinade"})
	if err != nil {
		t.Fatalf("move note: %v", err)
	}
	if moved.ID != "pargit_marinade" {
		t.Fatalf("unexpected moved note: %#v", moved)
	}
	if _, err := os.Stat(filepath.Join(root, "recipes", "pargit_marinade.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source note removed, got %v", err)
	}
	if !strings.Contains(moved.Content, "![ingredients](assets/recipes/pargit_marinade/image.png)") {
		t.Fatalf("markdown image link was not rewritten:\n%s", moved.Content)
	}
	if !strings.Contains(moved.Content, `src="assets/recipes/pargit_marinade/photo.png"`) {
		t.Fatalf("html image link was not rewritten:\n%s", moved.Content)
	}
	if !strings.Contains(moved.Content, "https://example.com/remote.png") {
		t.Fatalf("remote image link should remain unchanged:\n%s", moved.Content)
	}
}

func TestServiceSaveDeleteRecentAndUIState(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n")
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if _, err := service.SaveNote(ctx, SaveNoteRequest{ID: "new/note", Content: "---\ntype: concept\ntitle: New Note\n---\n\n# New Note\n"}); err != nil {
		t.Fatalf("save note: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new", "note.md")); err != nil {
		t.Fatalf("expected saved note file: %v", err)
	}
	if err := service.DeleteNote(ctx, "new/note"); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new", "note.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected note deleted, got %v", err)
	}
	recent, err := service.RecentWorkspaces(ctx)
	if err != nil {
		t.Fatalf("recent workspaces: %v", err)
	}
	if len(recent) != 1 || !workspacePathEqual(recent[0].Path, root) {
		t.Fatalf("unexpected recent workspaces: %#v", recent)
	}
	if err := service.SaveUIState(ctx, UIState{"lastNote": "alpha"}); err != nil {
		t.Fatalf("save ui state: %v", err)
	}
	state, err := service.LoadUIState(ctx)
	if err != nil {
		t.Fatalf("load ui state: %v", err)
	}
	if state["lastNote"] != "alpha" {
		t.Fatalf("unexpected ui state: %#v", state)
	}
	layout := LayoutSnapshotDTO{Coordinates: map[string]LayoutCoordinatesDTO{"alpha": {X: 1.5, Y: -2.25}}}
	if err := service.SaveGraphLayout(ctx, layout); err != nil {
		t.Fatalf("save graph layout: %v", err)
	}
	loadedLayout, err := service.LoadGraphLayout(ctx)
	if err != nil {
		t.Fatalf("load graph layout: %v", err)
	}
	if loadedLayout.Coordinates["alpha"].X != 1.5 || loadedLayout.Coordinates["alpha"].Y != -2.25 || loadedLayout.UpdatedAt == "" {
		t.Fatalf("unexpected graph layout: %#v", loadedLayout)
	}
	if _, err := os.Stat(filepath.Join(root, ".workspace", "layout", "graph-layout.json")); err != nil {
		t.Fatalf("expected graph layout file: %v", err)
	}
}

func TestServiceSettingsAreAppLevel(t *testing.T) {
	service := testService(t, nil)
	ctx := context.Background()

	settings, err := service.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("load default settings: %v", err)
	}
	if settings.Appearance.Theme != "dark" || settings.NoteView.DefaultEditMode != "rich" || settings.GraphView.DefaultDepth != 2 {
		t.Fatalf("unexpected default settings: %#v", settings)
	}

	settings.Appearance.Theme = "vscode-tokyo-night"
	settings.NoteView.DefaultEditMode = "source"
	settings.NoteView.ShowFindBar = false
	settings.GraphView.DefaultMode = "3d"
	settings.GraphView.DefaultDepth = 4
	settings.Workspaces = map[string]WorkspaceSettings{
		"  C:/Knowledge  ": {
			DefaultType:  "adr",
			EnabledTypes: []string{"concept", "adr"},
			AccessMode:   "readOnlyGit",
			GitURL:       " https://example.com/wiki.git ",
		},
	}
	if err := service.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	loaded, err := service.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("load saved settings: %v", err)
	}
	if loaded.Appearance.Theme != "vscode-tokyo-night" || loaded.NoteView.DefaultEditMode != "source" || loaded.NoteView.ShowFindBar || loaded.GraphView.DefaultMode != "3d" || loaded.GraphView.DefaultDepth != 4 {
		t.Fatalf("unexpected saved settings: %#v", loaded)
	}
	workspaceSettings, ok := loaded.Workspaces["C:/Knowledge"]
	if !ok {
		t.Fatalf("expected normalized workspace settings key: %#v", loaded.Workspaces)
	}
	if workspaceSettings.DefaultType != "adr" || workspaceSettings.AccessMode != "readOnlyGit" || workspaceSettings.GitURL != "https://example.com/wiki.git" {
		t.Fatalf("unexpected workspace settings: %#v", workspaceSettings)
	}
	if filepath.Base(service.settingsPath) != "GoMental.Settings.json" {
		t.Fatalf("unexpected settings path: %s", service.settingsPath)
	}
}

func TestServiceImportsRecipeURL(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html><head>
<title>Recipe page</title>
<script type="application/ld+json">
{
  "@context": "https://schema.org",
  "@type": "Recipe",
  "name": "Pan Toast",
  "description": "Simple toast.",
  "totalTime": "PT5M",
  "recipeIngredient": ["1 slice bread", "1 tsp butter"],
  "recipeInstructions": [
    {"@type": "HowToStep", "text": "Butter the bread."},
    {"@type": "HowToStep", "text": "Toast in a pan."}
  ]
}
</script>
</head></html>`))
	}))
	defer server.Close()

	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	imported, err := service.ImportURL(ctx, ImportURLRequest{URL: server.URL + "/toast"})
	if err != nil {
		t.Fatalf("import url: %v", err)
	}
	if imported.ID != "recipes/pan-toast" {
		t.Fatalf("unexpected imported note ID: %#v", imported)
	}
	if !strings.Contains(imported.Content, "type: recipe") || !strings.Contains(imported.Content, "1. Toast in a pan.") {
		t.Fatalf("unexpected imported content:\n%s", imported.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "recipes", "pan-toast.md")); err != nil {
		t.Fatalf("expected imported note file: %v", err)
	}
	results, err := service.Search(ctx, SearchQueryDTO{Text: "Toast", Limit: 10})
	if err != nil {
		t.Fatalf("search imported note: %v", err)
	}
	if len(results) == 0 || results[0].ID != "recipes/pan-toast" {
		t.Fatalf("imported note not indexed: %#v", results)
	}
}

func TestServiceProcessesExternalFilesystemChangesIncrementally(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\nSee [Beta](beta.md).\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\n# Beta\n")
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	writeNote(t, root, "gamma.md", "---\ntype: concept\ntitle: Gamma\n---\n\n# Gamma\nExternal note linking [Beta](beta.md).\n")
	if err := service.processWorkspaceChanges(ctx, platform.WorkspaceChangeSet{Changed: []domain.NoteID{"gamma"}}); err != nil {
		t.Fatalf("process create: %v", err)
	}
	results, err := service.Search(ctx, SearchQueryDTO{Text: "Gamma", Limit: 10})
	if err != nil {
		t.Fatalf("search gamma: %v", err)
	}
	if len(results) == 0 || results[0].ID != "gamma" {
		t.Fatalf("gamma not indexed: %#v", results)
	}
	backlinks, err := service.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("backlinks beta: %v", err)
	}
	if len(backlinks) != 2 {
		t.Fatalf("expected alpha and gamma backlinks to beta, got %#v", backlinks)
	}

	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha Edited\n---\n\n# Alpha Edited\nSee [Gamma](gamma.md).\n")
	if err := service.processWorkspaceChanges(ctx, platform.WorkspaceChangeSet{Changed: []domain.NoteID{"alpha"}}); err != nil {
		t.Fatalf("process modify: %v", err)
	}
	backlinks, err = service.Backlinks(ctx, "gamma")
	if err != nil {
		t.Fatalf("backlinks gamma: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("expected alpha backlink to gamma, got %#v", backlinks)
	}

	if err := os.Remove(filepath.Join(root, "beta.md")); err != nil {
		t.Fatalf("remove beta: %v", err)
	}
	if err := service.processWorkspaceChanges(ctx, platform.WorkspaceChangeSet{Deleted: []domain.NoteID{"beta"}}); err != nil {
		t.Fatalf("process delete: %v", err)
	}
	results, err = service.Search(ctx, SearchQueryDTO{Text: "Beta", Limit: 10})
	if err != nil {
		t.Fatalf("search beta: %v", err)
	}
	if hasSearchResult(results, "beta") {
		t.Fatalf("deleted beta still indexed: %#v", results)
	}
	full, err := service.FullGraph(ctx, GraphFilterDTO{IncludeUnresolved: true})
	if err != nil {
		t.Fatalf("full graph: %v", err)
	}
	if !graphHasNode(full, "unresolved:beta.md") && !graphHasNode(full, "unresolved:beta") {
		t.Fatalf("expected unresolved beta node after delete, got %#v", full.Nodes)
	}
}
func TestServiceOpenWorkspaceRepairsMissingAndCorruptDerivedState(t *testing.T) {
	root := t.TempDir()
	alphaContent := "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\nSee [Beta](beta.md).\n"
	writeNote(t, root, "alpha.md", alphaContent)
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\n# Beta\n")
	ctx := context.Background()

	service := testService(t, nil)
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open with missing projections should repair: %v", err)
	}
	assertSearchHit(t, service, "Alpha", "alpha")
	if _, err := os.Stat(filepath.Join(root, ".workspace", "logs", "GoMental.log")); err != nil {
		t.Fatalf("expected recovery log: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "alpha.md")); got != alphaContent {
		t.Fatalf("OKF note content changed during repair: %q", got)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	if err := os.RemoveAll(search.WorkspaceSearchPath(root)); err != nil {
		t.Fatalf("remove search dir: %v", err)
	}
	if err := os.WriteFile(search.WorkspaceSearchPath(root), []byte("not a bleve index"), 0o644); err != nil {
		t.Fatalf("corrupt search projection: %v", err)
	}
	service = testService(t, nil)
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open with corrupt search should repair: %v", err)
	}
	assertSearchHit(t, service, "Beta", "beta")
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	graphPath := graph.GraphPath(root)
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatalf("create graph dir: %v", err)
	}
	if err := os.WriteFile(graphPath, []byte("not sqlite"), 0o644); err != nil {
		t.Fatalf("corrupt graph projection: %v", err)
	}
	service = testService(t, nil)
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open with corrupt graph should repair: %v", err)
	}
	backlinks, err := service.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("backlinks after graph repair: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].Source != "alpha" {
		t.Fatalf("expected repaired graph backlink, got %#v", backlinks)
	}
	if got := readFile(t, filepath.Join(root, "alpha.md")); got != alphaContent {
		t.Fatalf("OKF note content changed during graph repair: %q", got)
	}
}

func TestServiceOpenWorkspaceRepairsOldSearchSchema(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "ramen soup #1.md", "---\ntype: concept\ntitle: Soup Draft\n---\n\n# Soup Draft\nNoodle broth notes.\n")
	ctx := context.Background()

	service := testService(t, nil)
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	statePath := filepath.Join(root, ".workspace", "state", "rebuild.json")
	state, err := indexing.ReadState(statePath)
	if err != nil {
		t.Fatalf("read rebuild state: %v", err)
	}
	state.SearchSchemaVersion = search.SearchSchemaVersion - 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal rebuild state: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("downgrade rebuild state: %v", err)
	}

	service = testService(t, nil)
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open with old search schema should repair: %v", err)
	}
	assertSearchHit(t, service, "ramen", "ramen soup #1")
}

func TestSearchIndexesReadableNotesWithMissingType(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "simple-homemade-ramen-soup.md", `---
title: Simple Homemade Ramen Soup
tags:
  - ramen
  - soup
favorite: true
---

# Simple Homemade Ramen Soup

Cook the ramen noodles according to the package instructions.
Serve with Japanese cucumber salad and pickled vegetables.
`)
	service := testService(t, nil)
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	for _, query := range []SearchQueryDTO{
		{Text: "cucumber", Limit: 10},
		{Text: "ramen", Tags: []string{"soup"}, Limit: 10},
		{Text: "noodles", FavoritesOnly: true, Limit: 10},
	} {
		results, err := service.Search(ctx, query)
		if err != nil {
			t.Fatalf("search %#v: %v", query, err)
		}
		if len(results) != 1 || results[0].ID != "simple-homemade-ramen-soup" || !results[0].Favorite {
			t.Fatalf("expected malformed note search hit for %#v, got %#v", query, results)
		}
	}
	notes, err := service.ListNotes(ctx)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 || !notes[0].Favorite || len(notes[0].Tags) != 2 {
		t.Fatalf("expected favorite/tags projected for malformed note, got %#v", notes)
	}
}

func TestServiceOpenWorkspaceDefersCorpusBuild(t *testing.T) {
	root := t.TempDir()
	// alpha's body mentions Beta's title; title-only soft inference should link
	// alpha -> beta only once the FULL corpus (carrying titles) is built in the
	// background — the ID-only index installed synchronously has no titles.
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\n---\n\n# Alpha\nThis note discusses Beta at length.\n")
	writeNote(t, root, "beta.md", "---\ntype: concept\ntitle: Beta\n---\n\n# Beta\nStandalone.\n")

	ready := make(chan struct{})
	var readyOnce sync.Once
	service := testService(t, func(name string, _ any) {
		if name == "corpus:ready" {
			readyOnce.Do(func() { close(ready) })
		}
	})
	ctx := context.Background()

	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	// The ID-only index is installed synchronously, so listing and link resolution
	// work immediately without waiting for the background parse.
	corpus := service.corpusState()
	if corpus == nil {
		t.Fatal("corpus not installed synchronously on open")
	}
	if ids := corpus.ResolverIDs(); len(ids) != 2 {
		t.Fatalf("expected 2 resolver ids immediately after open, got %d", len(ids))
	}
	if _, err := service.ListNotes(ctx); err != nil {
		t.Fatalf("list notes immediately after open: %v", err)
	}
	// A save on the hot path resolves a hard link against the ID-only index.
	if _, err := service.SaveNote(ctx, SaveNoteRequest{ID: "gamma", Content: "---\ntype: concept\ntitle: Gamma\n---\n\n# Gamma\nSee [Beta](beta.md).\n"}); err != nil {
		t.Fatalf("save note during background corpus build: %v", err)
	}
	backlinks, err := service.Backlinks(ctx, "beta")
	if err != nil {
		t.Fatalf("backlinks beta: %v", err)
	}
	var gammaResolved bool
	for _, bl := range backlinks {
		if bl.Source == "gamma" {
			gammaResolved = true
		}
	}
	if !gammaResolved {
		t.Fatalf("expected gamma hard-link to resolve against id-only index, got %#v", backlinks)
	}

	// Wait for the background full-corpus build to finish.
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("corpus:ready not emitted within timeout")
	}

	// The full corpus carries titles, so title-only inference now yields alpha->beta.
	infer := graph.NewLocalInferenceService(graph.InferenceConfig{})
	all, err := infer.InferAll(ctx, service.corpusState().Snapshot())
	if err != nil {
		t.Fatalf("infer all: %v", err)
	}
	if len(all["alpha"]) == 0 {
		t.Fatalf("expected full corpus to yield soft links from alpha, got none: %#v", all)
	}
}

func TestServiceReturnsStructuredErrorWhenWorkspaceMissing(t *testing.T) {
	service := testService(t, nil)
	_, err := service.ListNotes(context.Background())
	var appErr AppError
	if !errors.As(err, &appErr) || appErr.Code != "workspace.not_open" {
		t.Fatalf("expected structured workspace error, got %v", err)
	}
}

func testService(t *testing.T, events EventSink) *Service {
	t.Helper()
	store := workspace.NewRecentWorkspaceStore(filepath.Join(t.TempDir(), "recent.json"), 10)
	service := NewServiceWithStores(events, store, filepath.Join(t.TempDir(), "ui-state.json"))
	t.Cleanup(func() { _ = service.Close() })
	return service
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

func assertSearchHit(t *testing.T, service *Service, text string, id string) {
	t.Helper()
	results, err := service.Search(context.Background(), SearchQueryDTO{Text: text, Limit: 10})
	if err != nil {
		t.Fatalf("search %q: %v", text, err)
	}
	if !hasSearchResult(results, id) {
		t.Fatalf("expected search hit %q in %#v", id, results)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
func hasSearchResult(results []SearchResultDTO, id string) bool {
	for _, result := range results {
		if result.ID == id {
			return true
		}
	}
	return false
}
func graphHasNode(graph GraphDTO, id string) bool {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}
func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func workspacePathEqual(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return leftAbs == rightAbs
}
