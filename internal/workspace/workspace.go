package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"GoMental/internal/domain"
)

const DefaultMetadataDir = ".workspace"

type Workspace struct {
	root        string
	metadataDir string
}

func Open(root string) (Workspace, error) {
	return OpenWithMetadataDir(root, DefaultMetadataDir)
}

func OpenWithMetadataDir(root, metadataDir string) (Workspace, error) {
	if strings.TrimSpace(root) == "" {
		return Workspace{}, fmt.Errorf("%w: empty path", ErrInvalidWorkspaceRoot)
	}
	if metadataDir == "" || filepath.IsAbs(metadataDir) || strings.Contains(metadataDir, "..") || strings.ContainsAny(metadataDir, `/\\`) {
		return Workspace{}, fmt.Errorf("%w: invalid metadata directory %q", ErrInvalidWorkspaceRoot, metadataDir)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: %v", ErrInvalidWorkspaceRoot, err)
	}
	clean := filepath.Clean(abs)
	info, err := os.Stat(clean)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: %v", ErrInvalidWorkspaceRoot, err)
	}
	if !info.IsDir() {
		return Workspace{}, fmt.Errorf("%w: not a directory", ErrInvalidWorkspaceRoot)
	}
	return Workspace{root: clean, metadataDir: metadataDir}, nil
}

func (w Workspace) Root() string {
	return w.root
}

func (w Workspace) MetadataDirName() string {
	return w.metadataDir
}

func (w Workspace) MetadataPath() string {
	return filepath.Join(w.root, w.metadataDir)
}

func (w Workspace) NormalizeNoteID(raw string) (domain.NoteID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidNoteID)
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\\`) {
		return "", fmt.Errorf("%w: absolute path", ErrInvalidNoteID)
	}
	slashed := strings.ReplaceAll(raw, `\\`, "/")
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(slashed)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("%w: traversal", ErrInvalidNoteID)
	}
	if strings.HasSuffix(strings.ToLower(clean), ".md") {
		clean = clean[:len(clean)-3]
	}
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: empty", ErrInvalidNoteID)
	}
	if isReservedConceptID(clean) {
		return "", ErrReservedNoteID
	}
	return domain.NoteID(clean), nil
}

func (w Workspace) PathForNoteID(id domain.NoteID) (string, error) {
	normalized, err := w.NormalizeNoteID(string(id))
	if err != nil {
		return "", err
	}
	rel := filepath.FromSlash(string(normalized) + ".md")
	abs := filepath.Join(w.root, rel)
	if err := w.ensureInside(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func (w Workspace) NoteIDFromPath(path string) (domain.NoteID, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	if err := w.ensureInside(clean); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.root, clean)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", ErrPathEscapesWorkspace
	}
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		return "", fmt.Errorf("%w: not an OKF .md concept document", ErrInvalidNoteID)
	}
	slashed := filepath.ToSlash(rel)
	id := slashed[:len(slashed)-len(filepath.Ext(slashed))]
	return w.NormalizeNoteID(id)
}

func (w Workspace) IsMetadataPath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(w.root, filepath.Clean(abs))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	return strings.EqualFold(first, w.metadataDir)
}

func (w Workspace) ensureInside(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(w.root, filepath.Clean(abs))
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ErrPathEscapesWorkspace
	}
	return nil
}

func isReservedConceptID(id string) bool {
	lower := strings.ToLower(filepath.ToSlash(id))
	return lower == "index" || lower == "log"
}

func caseFoldKey(id domain.NoteID) string {
	return strings.ToLower(string(id))
}
