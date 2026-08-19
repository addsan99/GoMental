package application

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GoMental/internal/workspace"
)

// twoMemberCompositeService opens a service on a composite spanning two
// members, and returns the member roots in the order the composite lists them.
func twoMemberCompositeService(t *testing.T) (*Service, string, string) {
	t.Helper()
	base := t.TempDir()
	alpha := filepath.Join(base, "alpha")
	beta := filepath.Join(base, "beta")
	root := filepath.Join(base, "composite")
	for _, dir := range []string{alpha, beta, root} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNote(t, alpha, "existing-alpha.md", "---\ntype: term\n---\n\n# Existing Alpha\n")
	writeNote(t, beta, "existing-beta.md", "---\ntype: term\n---\n\n# Existing Beta\n")
	if err := workspace.WriteCompositeConfig(root, []string{alpha, beta}); err != nil {
		t.Fatal(err)
	}
	service := testService(t, nil)
	if _, err := service.OpenWorkspace(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	return service, alpha, beta
}

func TestSaveNoteWritesToTheChosenMember(t *testing.T) {
	service, alpha, beta := twoMemberCompositeService(t)
	ctx := context.Background()

	members, err := service.WorkspaceMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].Prefix != "alpha" || members[1].Prefix != "beta" {
		t.Fatalf("expected alpha and beta as destinations, got %+v", members)
	}

	chosen, err := service.SaveNote(ctx, SaveNoteRequest{
		ID:      "picked",
		Content: "---\ntype: term\n---\n\n# Picked\n",
		Member:  members[1].Prefix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if chosen.ID != "beta/picked" {
		t.Fatalf("expected the note in beta, got %q", chosen.ID)
	}
	if _, err := os.Stat(filepath.Join(beta, "picked.md")); err != nil {
		t.Fatalf("note is not on disk in the chosen member: %v", err)
	}

	// No choice still has to work: it falls back to the first member rather
	// than failing, because a composite root can hold no notes of its own.
	fallback, err := service.SaveNote(ctx, SaveNoteRequest{
		ID:      "unpicked",
		Content: "---\ntype: term\n---\n\n# Unpicked\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.ID != "alpha/unpicked" {
		t.Fatalf("expected the first member, got %q", fallback.ID)
	}
	if _, err := os.Stat(filepath.Join(alpha, "unpicked.md")); err != nil {
		t.Fatalf("note is not on disk in the first member: %v", err)
	}

	// Editing an existing note must never be re-homed by the destination logic.
	edited, err := service.SaveNote(ctx, SaveNoteRequest{
		ID:      "beta/existing-beta",
		Content: "---\ntype: term\n---\n\n# Edited\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID != "beta/existing-beta" {
		t.Fatalf("editing moved the note to %q", edited.ID)
	}
}

func TestCreateNoteRejectsAMemberThatIsNotThere(t *testing.T) {
	service, _, _ := twoMemberCompositeService(t)
	_, err := service.CreateNote(context.Background(), CreateNoteRequest{
		ID:      "nowhere",
		Content: "---\ntype: term\n---\n\n# Nowhere\n",
		Member:  "gamma",
	})
	if err == nil {
		t.Fatal("expected an error for a member that is not part of the composite")
	}
}

func TestNoteAssetsLandBesideTheNoteInItsMember(t *testing.T) {
	service, _, beta := twoMemberCompositeService(t)
	ctx := context.Background()

	// A 1x1 PNG, the smallest thing the image sniffer will accept.
	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.SaveNoteAsset(ctx, SaveNoteAssetRequest{
		NoteID:     "beta/existing-beta",
		FileName:   "shot.png",
		MIMEType:   "image/png",
		DataBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The link is written into the note, so it has to be a short relative hop,
	// not a climb out of the member and across to the composite root.
	if strings.HasPrefix(saved.Path, "..") {
		t.Fatalf("asset link escapes the member workspace: %q", saved.Path)
	}
	onDisk := filepath.Join(beta, filepath.FromSlash(saved.Path))
	if _, err := os.Stat(onDisk); err != nil {
		t.Fatalf("asset was not written into the owning member: %v", err)
	}
	// And it has to load back through the same path resolution.
	if _, err := service.LoadNoteAssetDataURL(ctx, NoteAssetRequest{
		NoteID: "beta/existing-beta",
		Path:   saved.Path,
	}); err != nil {
		t.Fatalf("could not read the asset back: %v", err)
	}
}

func TestRenamingANoteKeepsItInItsOwnMember(t *testing.T) {
	service, alpha, beta := twoMemberCompositeService(t)
	ctx := context.Background()

	renamed, err := service.MoveNote(ctx, MoveNoteRequest{ID: "beta/existing-beta", NewID: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	// A bare new name is a rename, not a request to move house: falling back to
	// the first member would quietly relocate the note to another workspace.
	if renamed.ID != "beta/renamed" {
		t.Fatalf("rename moved the note to %q", renamed.ID)
	}
	if _, err := os.Stat(filepath.Join(beta, "renamed.md")); err != nil {
		t.Fatalf("renamed note is not in its own member: %v", err)
	}
	if _, err := os.Stat(filepath.Join(alpha, "renamed.md")); err == nil {
		t.Fatal("renamed note leaked into the first member")
	}

	// Naming a member outright is still a deliberate move across members.
	moved, err := service.MoveNote(ctx, MoveNoteRequest{ID: "beta/renamed", NewID: "alpha/relocated"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != "alpha/relocated" {
		t.Fatalf("expected a cross-member move, got %q", moved.ID)
	}
	if _, err := os.Stat(filepath.Join(alpha, "relocated.md")); err != nil {
		t.Fatalf("moved note is not in the target member: %v", err)
	}
}

// On a composite, links resolve inside one member, so inbound repair has to work
// in the member's local ID space rather than the namespaced one on show.
func TestMoveNoteRepairsInboundLinksWithinAMember(t *testing.T) {
	service, alpha, _ := twoMemberCompositeService(t)
	ctx := context.Background()

	writeNote(t, alpha, "notes/target.md", "---\ntype: term\n---\n\n# Target\n")
	writeNote(t, alpha, "notes/linker.md", "---\ntype: term\n---\n\n[[notes/target]]\n[rel](target)\n")
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := service.MoveNote(ctx, MoveNoteRequest{ID: "alpha/notes/target", NewID: "alpha/archive/target"}); err != nil {
		t.Fatalf("move note: %v", err)
	}

	linker, err := service.ReadNote(ctx, "alpha/notes/linker")
	if err != nil {
		t.Fatalf("read linker: %v", err)
	}
	// Rewritten targets stay member-local: the "alpha/" prefix is a view of the
	// composite, not part of what the member's own links address.
	for _, want := range []string{"[[archive/target]]", "[rel](../archive/target)"} {
		if !strings.Contains(linker.Content, want) {
			t.Fatalf("expected %q after move:\n%s", want, linker.Content)
		}
	}
	links, err := service.Backlinks(ctx, "alpha/archive/target")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected both links repaired, got %#v", links)
	}
}

// A move that lands in a different member cannot be repaired: links never
// resolved across members, so the link is left exactly as the author wrote it.
func TestMoveNoteAcrossMembersLeavesInboundLinksAlone(t *testing.T) {
	service, alpha, _ := twoMemberCompositeService(t)
	ctx := context.Background()

	writeNote(t, alpha, "notes/target.md", "---\ntype: term\n---\n\n# Target\n")
	writeNote(t, alpha, "notes/linker.md", "---\ntype: term\n---\n\n[[notes/target]]\n")
	if _, err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}

	moved, err := service.MoveNote(ctx, MoveNoteRequest{ID: "alpha/notes/target", NewID: "beta/notes/target"})
	if err != nil {
		t.Fatalf("move note: %v", err)
	}
	if moved.ID != "beta/notes/target" {
		t.Fatalf("expected the note to land in beta, got %q", moved.ID)
	}
	linker, err := service.ReadNote(ctx, "alpha/notes/linker")
	if err != nil {
		t.Fatalf("read linker: %v", err)
	}
	if !strings.Contains(linker.Content, "[[notes/target]]") {
		t.Fatalf("cross-member move should not rewrite the link:\n%s", linker.Content)
	}
}
