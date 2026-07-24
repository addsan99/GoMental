package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"GoMental/internal/domain"
)

type FileNoteRepository struct {
	workspace Workspace
}

func NewFileNoteRepository(workspace Workspace) *FileNoteRepository {
	return &FileNoteRepository{workspace: workspace}
}

func (r *FileNoteRepository) List(ctx context.Context) ([]domain.NoteSummary, error) {
	scanned, err := r.workspace.ScanNotes(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]domain.NoteSummary, 0, len(scanned))
	for _, note := range scanned {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		summaries = append(summaries, domain.NoteSummary{
			ID:         note.ID,
			Title:      TitleFromID(note.ID),
			Path:       note.DisplayPath,
			ModifiedAt: note.ModifiedAt,
		})
	}
	return summaries, nil
}

func (r *FileNoteRepository) Read(ctx context.Context, id domain.NoteID) (domain.Note, error) {
	select {
	case <-ctx.Done():
		return domain.Note{}, ctx.Err()
	default:
	}
	path, err := r.workspace.PathForNoteID(id)
	if err != nil {
		return domain.Note{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Note{}, err
	}
	if !utf8.Valid(data) {
		return domain.Note{}, fmt.Errorf("%w: %s", ErrInvalidUTF8, id)
	}
	info, err := os.Stat(path)
	if err != nil {
		return domain.Note{}, err
	}
	rel, err := filepath.Rel(r.workspace.Root(), path)
	if err != nil {
		return domain.Note{}, err
	}
	normalized, err := r.workspace.NormalizeNoteID(string(id))
	if err != nil {
		return domain.Note{}, err
	}
	return domain.Note{
		ID:         normalized,
		Path:       domain.NotePath(filepath.ToSlash(rel)),
		Document:   domain.OKFDocument{Raw: string(data)},
		ModifiedAt: info.ModTime(),
		Version: domain.FileVersion{
			ModifiedAt: info.ModTime(),
			Size:       info.Size(),
		},
	}, nil
}

func (r *FileNoteRepository) Save(ctx context.Context, note domain.Note) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if !utf8.ValidString(note.Document.Raw) {
		return fmt.Errorf("%w: %s", ErrInvalidUTF8, note.ID)
	}
	path, err := r.workspace.PathForNoteID(note.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(note.Document.Raw), 0o644)
}

// SaveIfUnchanged writes note only if the file's current on-disk version matches
// expected (optimistic concurrency). A zero expected version means "the caller
// read no prior version" and is treated as: succeed only if the file does not
// yet exist (a create). If the file changed underneath the caller — or was
// created/deleted since — it returns ErrVersionConflict and does not write.
//
// This is the version-checked counterpart to Save; Save itself remains an
// unconditional last-write-wins write used by the single-user desktop path.
func (r *FileNoteRepository) SaveIfUnchanged(ctx context.Context, note domain.Note, expected domain.FileVersion) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	path, err := r.workspace.PathForNoteID(note.ID)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		current := domain.FileVersion{ModifiedAt: info.ModTime(), Size: info.Size()}
		if !fileVersionsEqual(current, expected) {
			return ErrVersionConflict
		}
	case os.IsNotExist(statErr):
		// File is gone (or never existed). If the caller expected a concrete
		// version, someone deleted/renamed it — that is a conflict.
		if !fileVersionIsZero(expected) {
			return ErrVersionConflict
		}
	default:
		return statErr
	}
	return r.Save(ctx, note)
}

func fileVersionsEqual(a, b domain.FileVersion) bool {
	return a.Size == b.Size && a.ModifiedAt.UnixNano() == b.ModifiedAt.UnixNano()
}

func fileVersionIsZero(v domain.FileVersion) bool {
	return v.Size == 0 && v.ModifiedAt.IsZero()
}

func (r *FileNoteRepository) Delete(ctx context.Context, id domain.NoteID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	path, err := r.workspace.PathForNoteID(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r *FileNoteRepository) Rename(ctx context.Context, oldID domain.NoteID, newID domain.NoteID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	oldPath, err := r.workspace.PathForNoteID(oldID)
	if err != nil {
		return err
	}
	newPath, err := r.workspace.PathForNoteID(newID)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("%w: %s", ErrNoteAlreadyExists, newID)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}
// TitleFromID derives a display title from a note ID's filename (dashes and
// underscores become spaces). It is the canonical list/sidebar title used by
// FileNoteRepository.List and the SQLite note-metadata projection.
func TitleFromID(id domain.NoteID) string {
	parts := strings.Split(string(id), "/")
	base := parts[len(parts)-1]
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return base
}
