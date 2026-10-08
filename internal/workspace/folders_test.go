package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathForFolderResolvesInsideTheWorkspace(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "topics/deep/note.md", "# Note\n")
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ws.PathForFolder("topics/deep")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "topics", "deep"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// The top level has no folder segment, so "" has to resolve rather than fail.
	got, err = ws.PathForFolder("")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(root) {
		t.Fatalf("got %q, want workspace root %q", got, root)
	}
}

func TestPathForFolderRejectsEscapes(t *testing.T) {
	ws, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"..", "../outside", "topics/../../outside", "/etc"} {
		if _, err := ws.PathForFolder(folder); err == nil {
			t.Fatalf("expected %q to be rejected", folder)
		}
	}
}

func TestPathForFolderResolvesIntoCompositeMembers(t *testing.T) {
	ws, alphaRoot, _ := twoMemberComposite(t)

	// A composite's first segment names a member, so it must land in that
	// member's own root and not in a folder inside the composite.
	got, err := ws.PathForFolder("alpha/topics")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(alphaRoot, "topics"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got, err = ws.PathForFolder("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(alphaRoot) {
		t.Fatalf("got %q, want member root %q", got, alphaRoot)
	}

	if _, err := ws.PathForFolder("nosuchmember/topics"); !errors.Is(err, ErrUnknownNotePrefix) {
		t.Fatalf("got %v, want ErrUnknownNotePrefix", err)
	}
}

func TestCreateFolderMakesADirectory(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "topics/note.md", "# Note\n")
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	folder, err := ws.CreateFolder("topics", "reference")
	if err != nil {
		t.Fatal(err)
	}
	if folder != "topics/reference" {
		t.Fatalf("got %q, want topics/reference", folder)
	}
	info, err := os.Stat(filepath.Join(root, "topics", "reference"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected a directory on disk: %v", err)
	}

	// Creating at the top level has no parent segment to prefix.
	folder, err = ws.CreateFolder("", "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if folder != "inbox" {
		t.Fatalf("got %q, want inbox", folder)
	}
}

func TestCreateFolderRefusesDuplicates(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateFolder("", "inbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateFolder("", "inbox"); !errors.Is(err, ErrFolderAlreadyExists) {
		t.Fatalf("got %v, want ErrFolderAlreadyExists", err)
	}
}

func TestCreateFolderRejectsUnusableNames(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// Each of these would either escape the workspace or create a folder the
	// note scanner skips, leaving the user with an invisible directory.
	for _, name := range []string{"", "   ", "a/b", `a\b`, ".", "..", ".hidden", "~scratch", "node_modules", DefaultMetadataDir} {
		if _, err := ws.CreateFolder("", name); !errors.Is(err, ErrInvalidFolder) {
			t.Fatalf("name %q: got %v, want ErrInvalidFolder", name, err)
		}
	}
}

func TestCreateFolderRequiresAnExistingParent(t *testing.T) {
	ws, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateFolder("nope", "child"); !errors.Is(err, ErrInvalidFolder) {
		t.Fatalf("got %v, want ErrInvalidFolder", err)
	}
}

func TestCreateFolderInCompositeNeedsAMember(t *testing.T) {
	ws, alphaRoot, _ := twoMemberComposite(t)

	// The composite root is a container for the member list; a folder there
	// would hold no notes, so it has to be refused rather than silently made.
	if _, err := ws.CreateFolder("", "inbox"); !errors.Is(err, ErrInvalidFolder) {
		t.Fatalf("got %v, want ErrInvalidFolder", err)
	}

	folder, err := ws.CreateFolder("alpha/topics", "reference")
	if err != nil {
		t.Fatal(err)
	}
	if folder != "alpha/topics/reference" {
		t.Fatalf("got %q, want alpha/topics/reference", folder)
	}
	if info, err := os.Stat(filepath.Join(alphaRoot, "topics", "reference")); err != nil || !info.IsDir() {
		t.Fatalf("expected the folder inside the member root: %v", err)
	}
}

func TestNormalizeFolderCleansSeparators(t *testing.T) {
	ws, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]string{
		"":              "",
		"  ":            "",
		".":             "",
		"topics":        "topics",
		"topics/":       "topics",
		"  topics/sub ": "topics/sub",
		"topics/./sub":  "topics/sub",
	} {
		got, err := ws.NormalizeFolder(raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if got != want {
			t.Fatalf("%q: got %q, want %q", raw, got, want)
		}
	}
}
