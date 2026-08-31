package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuggestTagsUsesFieldStrength(t *testing.T) {
	vocab := map[string]int{"kubernetes": 3, "postgres": 2, "retrospective": 2}
	got := suggestTags(autoTagInput{
		Title:    "Kubernetes upgrade plan",
		Headings: []string{"Postgres migration"},
		Body:     "We only mention retrospective once here.",
	}, vocab, 40)
	if len(got) != 2 {
		t.Fatalf("expected two tags, got %#v", got)
	}
	if got[0].Tag != "kubernetes" || got[1].Tag != "postgres" {
		t.Fatalf("unexpected ranking: %#v", got)
	}
	if got[0].Confidence <= got[1].Confidence {
		t.Fatalf("a title hit should outrank a heading hit: %#v", got)
	}
}

func TestSuggestTagsNeedsRepetitionForBodyOnlyMatches(t *testing.T) {
	vocab := map[string]int{"caching": 3}
	once := suggestTags(autoTagInput{Title: "Notes", Body: "caching is mentioned once"}, vocab, 40)
	if len(once) != 0 {
		t.Fatalf("a single body mention should not clear the bar: %#v", once)
	}
	body := strings.Repeat("caching matters. ", 5)
	often := suggestTags(autoTagInput{Title: "Notes", Body: body}, vocab, 40)
	if len(often) != 1 || often[0].Tag != "caching" {
		t.Fatalf("repeated body mentions should qualify: %#v", often)
	}
}

func TestSuggestTagsMatchesWholeWordsOnly(t *testing.T) {
	vocab := map[string]int{"cat": 2}
	got := suggestTags(autoTagInput{Title: "Concatenation rules", Body: "concatenate everything"}, vocab, 40)
	if len(got) != 0 {
		t.Fatalf("substring match leaked through: %#v", got)
	}
}

func TestSuggestTagsRejectsShortTagsOutsideTitle(t *testing.T) {
	vocab := map[string]int{"go": 4}
	body := strings.Repeat("we go there and go back. ", 6)
	if got := suggestTags(autoTagInput{Title: "Weekend trip", Body: body}, vocab, 40); len(got) != 0 {
		t.Fatalf("short tag matched outside the title: %#v", got)
	}
	if got := suggestTags(autoTagInput{Title: "Go concurrency", Body: "nothing"}, vocab, 40); len(got) != 1 {
		t.Fatalf("short tag should still match in the title: %#v", got)
	}
}

func TestSuggestTagsDiscountsUbiquitousTags(t *testing.T) {
	vocab := map[string]int{"note": 38}
	got := suggestTags(autoTagInput{Title: "Note taking", Body: ""}, vocab, 40)
	if len(got) != 0 {
		t.Fatalf("a tag on 95%% of notes carries no signal: %#v", got)
	}
	common := suggestTags(autoTagInput{Title: "Note taking", Body: ""}, map[string]int{"note": 26}, 40)
	if len(common) != 0 {
		t.Fatalf("a common tag should be halved below the bar: %#v", common)
	}
}

func TestSuggestTagsMatchesMultiWordTags(t *testing.T) {
	vocab := map[string]int{"how-to": 3}
	got := suggestTags(autoTagInput{Title: "A how to guide", Body: ""}, vocab, 40)
	if len(got) != 1 || got[0].Tag != "how-to" {
		t.Fatalf("hyphenated tag should match its spaced form: %#v", got)
	}
}

func TestSuggestTagsIgnoresCodeBlocks(t *testing.T) {
	vocab := map[string]int{"postgres": 3}
	body := "```\n" + strings.Repeat("postgres postgres\n", 8) + "```\n"
	if got := suggestTags(autoTagInput{Title: "Setup", Body: body}, vocab, 40); len(got) != 0 {
		t.Fatalf("code block text should not produce tags: %#v", got)
	}
}

func TestSuggestTagsCapsResults(t *testing.T) {
	vocab := map[string]int{}
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"}
	for _, word := range words {
		vocab[word] = 2
	}
	got := suggestTags(autoTagInput{Title: strings.Join(words, " ")}, vocab, 40)
	if len(got) != autoTagMaxTags {
		t.Fatalf("expected the result capped at %d, got %d", autoTagMaxTags, len(got))
	}
}

func TestEnsureTagsFrontmatter(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{"adds to existing frontmatter", "---\ntype: general\n---\n\n# A\n", "---\ntype: general\ntags: []\n---\n\n# A\n", true},
		{"creates frontmatter", "# A\n", "---\ntags: []\n---\n# A\n", true},
		{"leaves inline tags alone", "---\ntags: [go]\n---\n\n# A\n", "---\ntags: [go]\n---\n\n# A\n", false},
		{"leaves block tags alone", "---\ntags:\n  - go\n---\n\n# A\n", "---\ntags:\n  - go\n---\n\n# A\n", false},
		{"leaves a bare tags key alone", "---\ntags:\n---\n\n# A\n", "---\ntags:\n---\n\n# A\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := ensureTagsFrontmatter(tc.in)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, changed, tc.want, tc.changed)
			}
		})
	}
}

func TestSetTagsFrontmatterReplacesEmptyForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty array", "---\ntype: general\ntags: []\n---\n\n# A\n", "---\ntype: general\ntags: [alpha, beta]\n---\n\n# A\n"},
		{"bare key with orphan items", "---\ntype: general\ntags:\n  -\n---\n\n# A\n", "---\ntype: general\ntags: [alpha, beta]\n---\n\n# A\n"},
		{"missing key", "---\ntype: general\n---\n\n# A\n", "---\ntype: general\ntags: [alpha, beta]\n---\n\n# A\n"},
		{"no frontmatter", "# A\n", "---\ntags: [alpha, beta]\n---\n# A\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := setTagsFrontmatter(tc.in, []string{"alpha", "beta"})
			if !changed || got != tc.want {
				t.Fatalf("got (%q, %v), want %q", got, changed, tc.want)
			}
		})
	}
}

func TestSetTagsFrontmatterQuotesAwkwardTags(t *testing.T) {
	got, _ := setTagsFrontmatter("---\ntype: general\n---\n# A\n", []string{"a,b"})
	if !strings.Contains(got, `tags: ["a,b"]`) {
		t.Fatalf("tag with a comma must be quoted: %q", got)
	}
}

func TestNormalizeAutoTagDefaultsOn(t *testing.T) {
	if normalizeAutoTag("") != autoTagOn {
		t.Fatal("a missing value must default to on")
	}
	if normalizeAutoTag("nonsense") != autoTagOn {
		t.Fatal("an unrecognized value must default to on")
	}
	if normalizeAutoTag("off") != autoTagOff {
		t.Fatal("off must be preserved")
	}
}

func TestLooksLikeLocalImportPath(t *testing.T) {
	paths := []string{"/tmp/a.md", "~/notes/a.md", "./a.md", "../a.md", "file:///tmp/a.md"}
	for _, p := range paths {
		if !looksLikeLocalImportPath(p) {
			t.Fatalf("%q should be treated as a path", p)
		}
	}
	urls := []string{"https://example.com/a", "http://example.com", "example.com/a", ""}
	for _, u := range urls {
		if looksLikeLocalImportPath(u) {
			t.Fatalf("%q should not be treated as a path", u)
		}
	}
}

func TestReadImportableTextFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.md")
	if err := os.WriteFile(good, []byte("# A\r\nbody\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := readImportableTextFile(good)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw != "# A\nbody\n" {
		t.Fatalf("CRLF should be normalized, got %q", raw)
	}

	binary := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(binary, []byte("ok\x00nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readImportableTextFile(binary); err == nil {
		t.Fatal("a NUL byte should be rejected")
	}

	wrongExt := filepath.Join(dir, "c.png")
	if err := os.WriteFile(wrongExt, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readImportableTextFile(wrongExt); err == nil {
		t.Fatal("an unsupported extension should be rejected")
	}

	if _, err := readImportableTextFile(filepath.Join(dir, "missing.md")); err == nil {
		t.Fatal("a missing file should be rejected")
	}
	if _, err := readImportableTextFile(dir); err == nil {
		t.Fatal("a directory should be rejected")
	}
}

func TestLocalImportDocument(t *testing.T) {
	existing := "---\ntype: term\ntitle: Kept\n---\n\n# Kept\n"
	content, title, err := localImportDocument("/tmp/whatever.md", existing)
	if err != nil {
		t.Fatal(err)
	}
	if content != existing || title != "Kept" {
		t.Fatalf("existing frontmatter should survive: %q / %q", content, title)
	}

	content, title, err = localImportDocument("/tmp/my_meeting-notes.txt", "Just some prose.\n")
	if err != nil {
		t.Fatal(err)
	}
	if title != "My meeting notes" {
		t.Fatalf("unexpected derived title %q", title)
	}
	if !strings.HasPrefix(content, "---\n") || !strings.Contains(content, "title: My meeting notes") {
		t.Fatalf("frontmatter should be synthesized: %q", content)
	}
	if !strings.Contains(content, "# My meeting notes") {
		t.Fatalf("a heading should be added when the file has none: %q", content)
	}

	_, title, err = localImportDocument("/tmp/x.md", "# From heading\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	if title != "From heading" {
		t.Fatalf("the first heading should win over the filename, got %q", title)
	}
}

func TestLocalImportSlug(t *testing.T) {
	if got := localImportSlug("/tmp/My Meeting Notes.md", "Ignored"); got != "my-meeting-notes" {
		t.Fatalf("unexpected slug %q", got)
	}
	if got := localImportSlug("/tmp/___.md", "Fallback Title"); got != "fallback-title" {
		t.Fatalf("unexpected fallback slug %q", got)
	}
}

func TestNewAndImportedNotesAlwaysCarryTags(t *testing.T) {
	root := t.TempDir()
	// Two notes give the vocabulary a "kubernetes" tag to draw on and a corpus
	// size large enough that it is not treated as ubiquitous.
	writeNote(t, root, "alpha.md", "---\ntype: term\ntitle: Alpha\ntags: [kubernetes]\n---\n\n# Alpha\n")
	writeNote(t, root, "beta.md", "---\ntype: term\ntitle: Beta\ntags: [postgres]\n---\n\n# Beta\n")
	service := testService(t, func(string, any) {})
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	created, err := service.CreateNote(ctx, CreateNoteRequest{ID: "gamma", Content: "---\ntype: term\ntitle: Gamma\n---\n\n# Gamma\n"})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if !strings.Contains(created.Content, "tags: []") {
		t.Fatalf("a new note must carry an empty tags array: %q", created.Content)
	}

	saved, err := service.SaveNote(ctx, SaveNoteRequest{ID: "gamma", Content: "---\ntype: term\ntitle: Kubernetes rollout\ntags: []\n---\n\n# Kubernetes rollout\n"})
	if err != nil {
		t.Fatalf("save note: %v", err)
	}
	if !strings.Contains(saved.Content, "tags: [kubernetes]") {
		t.Fatalf("save should auto-tag from the workspace vocabulary: %q", saved.Content)
	}

	source := filepath.Join(t.TempDir(), "postgres-tuning.md")
	if err := os.WriteFile(source, []byte("# Postgres tuning\n\nNotes about the database.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := service.ImportURL(ctx, ImportURLRequest{URL: source})
	if err != nil {
		t.Fatalf("import local file: %v", err)
	}
	if imported.ID != "postgres-tuning" {
		t.Fatalf("unexpected imported id %q", imported.ID)
	}
	if !strings.Contains(imported.Content, "tags: [postgres]") {
		t.Fatalf("import should auto-tag: %q", imported.Content)
	}
}

func TestAutoTagRespectsWorkspaceSetting(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: term\ntitle: Alpha\ntags: [kubernetes]\n---\n\n# Alpha\n")
	service := testService(t, func(string, any) {})
	ctx := context.Background()
	if _, err := service.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	settings, err := service.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if settings.Workspaces == nil {
		settings.Workspaces = map[string]WorkspaceSettings{}
	}
	ws := settings.Workspaces[root]
	ws.AutoTag = autoTagOff
	settings.Workspaces[root] = ws
	if err := service.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	created, err := service.CreateNote(ctx, CreateNoteRequest{ID: "gamma", Content: "---\ntype: term\ntitle: Kubernetes rollout\n---\n\n# Kubernetes rollout\n"})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if strings.Contains(created.Content, "kubernetes]") {
		t.Fatalf("auto-tagging should be off: %q", created.Content)
	}
	if !strings.Contains(created.Content, "tags: []") {
		t.Fatalf("the empty tags array is not part of the toggle: %q", created.Content)
	}
}
