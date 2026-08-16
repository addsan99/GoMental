package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNote(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// twoMemberComposite builds a composite over two members that each contain a
// note with the same ID, which is the case that proves namespacing works.
func twoMemberComposite(t *testing.T) (composite Workspace, alphaRoot, betaRoot string) {
	t.Helper()
	base := t.TempDir()
	alphaRoot = filepath.Join(base, "alpha")
	betaRoot = filepath.Join(base, "beta")
	compositeRoot := filepath.Join(base, "all")
	for _, dir := range []string{alphaRoot, betaRoot, compositeRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNote(t, alphaRoot, "shared.md", "# Alpha shared\n")
	writeNote(t, alphaRoot, "topics/only-alpha.md", "# Only alpha\n")
	writeNote(t, betaRoot, "shared.md", "# Beta shared\n")

	if err := WriteCompositeConfig(compositeRoot, []string{alphaRoot, betaRoot}); err != nil {
		t.Fatal(err)
	}
	ws, err := Open(compositeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !ws.IsComposite() {
		t.Fatal("expected a composite workspace")
	}
	return ws, alphaRoot, betaRoot
}

func TestCompositeScanNamespacesEveryMember(t *testing.T) {
	ws, _, _ := twoMemberComposite(t)
	notes, err := ws.ScanNotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, note := range notes {
		got[string(note.ID)] = true
	}
	for _, want := range []string{"alpha/shared", "alpha/topics/only-alpha", "beta/shared"} {
		if !got[want] {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d (%v)", len(notes), got)
	}
}

func TestCompositeRoutesPathsToOwningMember(t *testing.T) {
	ws, alphaRoot, betaRoot := twoMemberComposite(t)

	alphaPath, err := ws.PathForNoteID("alpha/shared")
	if err != nil {
		t.Fatal(err)
	}
	if alphaPath != filepath.Join(alphaRoot, "shared.md") {
		t.Fatalf("alpha/shared resolved to %s", alphaPath)
	}
	betaPath, err := ws.PathForNoteID("beta/shared")
	if err != nil {
		t.Fatal(err)
	}
	if betaPath != filepath.Join(betaRoot, "shared.md") {
		t.Fatalf("beta/shared resolved to %s", betaPath)
	}

	// The same-named notes must not collapse onto one file.
	if alphaPath == betaPath {
		t.Fatal("members share a path for identically named notes")
	}

	roundTripped, err := ws.NoteIDFromPath(betaPath)
	if err != nil {
		t.Fatal(err)
	}
	if roundTripped != "beta/shared" {
		t.Fatalf("round trip gave %q", roundTripped)
	}
}

func TestCompositeRejectsUnknownPrefixAndOutsidePaths(t *testing.T) {
	ws, _, _ := twoMemberComposite(t)
	if _, err := ws.PathForNoteID("gamma/shared"); err == nil {
		t.Fatal("expected an error for an unknown member prefix")
	}
	if _, err := ws.PathForNoteID("shared"); err == nil {
		t.Fatal("expected an error for an unqualified note id")
	}
	if _, err := ws.NoteIDFromPath(filepath.Join(t.TempDir(), "elsewhere.md")); err == nil {
		t.Fatal("expected an error for a path outside every member")
	}
}

func TestCompositePlacesNewNotesInTheFirstMember(t *testing.T) {
	ws, _, _ := twoMemberComposite(t)
	qualified, err := ws.QualifyNewNoteID("brand-new")
	if err != nil {
		t.Fatal(err)
	}
	if qualified != "alpha/brand-new" {
		t.Fatalf("expected the first member, got %q", qualified)
	}
	// An ID that already names a member is left where the caller put it.
	kept, err := ws.QualifyNewNoteID("beta/brand-new")
	if err != nil {
		t.Fatal(err)
	}
	if kept != "beta/brand-new" {
		t.Fatalf("expected the id to be preserved, got %q", kept)
	}
}

func TestCompositePrefixesSurviveMemberListEdits(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	root := filepath.Join(base, "all")
	for _, dir := range []string{first, second, root} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteCompositeConfig(root, []string{first, second}); err != nil {
		t.Fatal(err)
	}
	// Reordering and dropping a member must not renumber the survivors, or every
	// saved layout and starred note keyed by note ID would break.
	if err := WriteCompositeConfig(root, []string{second}); err != nil {
		t.Fatal(err)
	}
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	members := ws.Members()
	if len(members) != 1 || members[0].Prefix != "second" {
		t.Fatalf("unexpected members after edit: %+v", members)
	}
}

func TestCompositeCannotNest(t *testing.T) {
	base := t.TempDir()
	inner := filepath.Join(base, "inner")
	leaf := filepath.Join(base, "leaf")
	outer := filepath.Join(base, "outer")
	for _, dir := range []string{inner, leaf, outer} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteCompositeConfig(inner, []string{leaf}); err != nil {
		t.Fatal(err)
	}
	if err := WriteCompositeConfig(outer, []string{inner}); err == nil {
		t.Fatal("expected nesting a composite inside a composite to be rejected")
	}
}

func TestCompositeSkipsMembersThatAreGone(t *testing.T) {
	ws, _, betaRoot := twoMemberComposite(t)
	if err := os.RemoveAll(betaRoot); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ws.Root())
	if err != nil {
		t.Fatalf("a missing member should not fail the composite: %v", err)
	}
	members := reopened.Members()
	if len(members) != 1 || members[0].Prefix != "alpha" {
		t.Fatalf("expected only alpha to survive, got %+v", members)
	}
	notes, err := reopened.ScanNotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected alpha's 2 notes, got %d", len(notes))
	}
}

func TestCompositeHonoursAnExplicitDestinationMember(t *testing.T) {
	ws, _, betaRoot := twoMemberComposite(t)

	chosen, err := ws.QualifyNewNoteIDIn("brand-new", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if chosen != "beta/brand-new" {
		t.Fatalf("expected the chosen member, got %q", chosen)
	}
	path, err := ws.PathForNoteID(chosen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, betaRoot) {
		t.Fatalf("note landed at %q, expected it inside %q", path, betaRoot)
	}

	// A folder that happens to share another member's name must not steal the
	// note away from the member the user actually picked.
	nested, err := ws.QualifyNewNoteIDIn("alpha/brand-new", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if nested != "beta/alpha/brand-new" {
		t.Fatalf("expected the explicit member to win, got %q", nested)
	}

	if _, err := ws.QualifyNewNoteIDIn("brand-new", "gamma"); !errors.Is(err, ErrUnknownNotePrefix) {
		t.Fatalf("expected an unknown-prefix error for a member that is not there, got %v", err)
	}
}

func TestOrdinaryWorkspaceIgnoresADestinationMember(t *testing.T) {
	ws, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := ws.QualifyNewNoteIDIn("brand-new", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if id != "brand-new" {
		t.Fatalf("expected the id untouched on an ordinary workspace, got %q", id)
	}
}
