package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"GoMental/internal/domain"
)

func TestWorkspaceNoteIDNormalizationAndPathResolution(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	id, err := ws.NormalizeNoteID(`Folder\\My Note.md`)
	if err != nil {
		t.Fatalf("normalize note id: %v", err)
	}
	if id != "Folder/My Note" {
		t.Fatalf("expected normalized id, got %q", id)
	}

	path, err := ws.PathForNoteID(id)
	if err != nil {
		t.Fatalf("path for note id: %v", err)
	}
	want := filepath.Join(root, "Folder", "My Note.md")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestWorkspaceRejectsTraversalAndReservedIDs(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	invalid := []string{"../escape", "notes/../../escape", "/absolute", `C:\\absolute`, "index", "log.md"}
	for _, raw := range invalid {
		if _, err := ws.NormalizeNoteID(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestNoteIDFromPathRejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\ntype: concept\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ws.NoteIDFromPath(outside)
	if !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Fatalf("expected path escape error, got %v", err)
	}
}

func TestScanNotesExcludesMetadataReservedAndTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.md", "---\ntype: concept\n---\n# Alpha\n")
	writeFile(t, root, "folder", "beta.md", "---\ntype: concept\n---\n# Beta\n")
	writeFile(t, root, ".workspace", "hidden.md", "ignored")
	writeFile(t, root, "index.md", "reserved")
	writeFile(t, root, "log.md", "reserved")
	writeFile(t, root, "~$draft.md", "ignored")
	writeFile(t, root, ".hidden.md", "ignored")
	writeFile(t, root, "not-markdown.txt", "ignored")

	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	notes, err := ws.ScanNotes(context.Background())
	if err != nil {
		t.Fatalf("scan notes: %v", err)
	}
	got := ids(notes)
	want := []domain.NoteID{"alpha", "folder/beta"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestCaseFoldKeyUsesWindowsFirstCollisionPolicy(t *testing.T) {
	if caseFoldKey("Folder/Alpha") != caseFoldKey("folder/alpha") {
		t.Fatal("expected note IDs to collide case-insensitively")
	}
}

func TestFileNoteRepositoryCRUD(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	repo := NewFileNoteRepository(ws)
	ctx := context.Background()

	note := domain.Note{
		ID:       "notes/first",
		Document: domain.OKFDocument{Raw: "---\ntype: concept\ntitle: First\n---\n\n# First\n"},
	}
	if err := repo.Save(ctx, note); err != nil {
		t.Fatalf("save note: %v", err)
	}

	read, err := repo.Read(ctx, "notes/first")
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if read.ID != "notes/first" || read.Document.Raw != note.Document.Raw {
		t.Fatalf("unexpected read note: %#v", read)
	}
	if read.Path != "notes/first.md" {
		t.Fatalf("expected display path notes/first.md, got %q", read.Path)
	}

	summaries, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "notes/first" {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}

	if err := repo.Delete(ctx, "notes/first"); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if _, err := repo.Read(ctx, "notes/first"); !os.IsNotExist(err) {
		t.Fatalf("expected missing note after delete, got %v", err)
	}
}

func TestFileNoteRepositoryRejectsInvalidUTF8(t *testing.T) {
	root := t.TempDir()
	writeBytes(t, root, "bad.md", []byte{0xff, 0xfe})
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	repo := NewFileNoteRepository(ws)
	_, err = repo.Read(context.Background(), "bad")
	if !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("expected invalid utf8 error, got %v", err)
	}
}

func TestFileNoteRepositoryRename(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	repo := NewFileNoteRepository(ws)
	ctx := context.Background()

	if err := repo.Save(ctx, domain.Note{ID: "old/name", Document: domain.OKFDocument{Raw: "---\ntype: concept\n---\n"}}); err != nil {
		t.Fatalf("save old note: %v", err)
	}
	if err := repo.Rename(ctx, "old/name", "new/name"); err != nil {
		t.Fatalf("rename note: %v", err)
	}
	if _, err := repo.Read(ctx, "old/name"); !os.IsNotExist(err) {
		t.Fatalf("expected old note to be missing, got %v", err)
	}
	if _, err := repo.Read(ctx, "new/name"); err != nil {
		t.Fatalf("expected renamed note to be readable: %v", err)
	}
}

func TestFileNoteRepositoryRenameRejectsExistingTarget(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	repo := NewFileNoteRepository(ws)
	ctx := context.Background()

	if err := repo.Save(ctx, domain.Note{ID: "source", Document: domain.OKFDocument{Raw: "---\ntype: concept\n---\nsource"}}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	if err := repo.Save(ctx, domain.Note{ID: "target", Document: domain.OKFDocument{Raw: "---\ntype: concept\n---\ntarget"}}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	err = repo.Rename(ctx, "source", "target")
	if !errors.Is(err, ErrNoteAlreadyExists) {
		t.Fatalf("expected existing target error, got %v", err)
	}
}

func TestRecentWorkspaceStorePersistsMostRecentFirst(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "recent.json")
	store := NewRecentWorkspaceStore(storePath, 2)
	ctx := context.Background()

	if err := store.Add(ctx, rootA); err != nil {
		t.Fatalf("add root a: %v", err)
	}
	if err := store.Add(ctx, rootB); err != nil {
		t.Fatalf("add root b: %v", err)
	}
	if err := store.Add(ctx, rootA); err != nil {
		t.Fatalf("add root a again: %v", err)
	}

	loaded, err := NewRecentWorkspaceStore(storePath, 2).List(ctx)
	if err != nil {
		t.Fatalf("list recent workspaces: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected two recent workspaces, got %#v", loaded)
	}
	if !samePath(loaded[0].Path, rootA) || !samePath(loaded[1].Path, rootB) {
		t.Fatalf("unexpected recent order: %#v", loaded)
	}
	if loaded[0].OpenedAt.IsZero() {
		t.Fatal("expected opened timestamp")
	}
}
func ids(notes []ScannedNote) []domain.NoteID {
	out := make([]domain.NoteID, len(notes))
	for i, note := range notes {
		out[i] = note.ID
	}
	return out
}

func writeFile(t *testing.T, root string, parts ...string) {
	t.Helper()
	content := parts[len(parts)-1]
	path := filepath.Join(append([]string{root}, parts[:len(parts)-1]...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBytes(t *testing.T, root string, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}
