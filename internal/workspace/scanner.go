package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"GoMental/internal/domain"
)

type ScannedNote struct {
	ID          domain.NoteID
	Path        string
	DisplayPath domain.NotePath
	ModifiedAt  time.Time
	Size        int64
}

func (w Workspace) ScanNotes(ctx context.Context) ([]ScannedNote, error) {
	var notes []ScannedNote
	seen := map[string]domain.NoteID{}
	err := filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if path == w.root {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) || w.IsMetadataPath(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(entry.Name()) || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		id, err := w.NoteIDFromPath(path)
		if err != nil {
			if err == ErrReservedNoteID {
				return nil
			}
			return err
		}
		key := caseFoldKey(id)
		if existing, ok := seen[key]; ok && existing != id {
			return fmt.Errorf("%w: %s conflicts with %s", ErrDuplicateNoteID, id, existing)
		}
		seen[key] = id
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(w.root, path)
		if err != nil {
			return err
		}
		notes = append(notes, ScannedNote{
			ID:          id,
			Path:        path,
			DisplayPath: domain.NotePath(filepath.ToSlash(rel)),
			ModifiedAt:  info.ModTime(),
			Size:        info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool {
		return strings.ToLower(string(notes[i].ID)) < strings.ToLower(string(notes[j].ID))
	})
	return notes, nil
}

func shouldSkipDir(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case ".git", "node_modules", DefaultMetadataDir:
		return true
	default:
		return strings.HasPrefix(name, "~")
	}
}

func shouldSkipFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") || strings.HasPrefix(name, "~") {
		return true
	}
	if strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".swp") || strings.HasSuffix(lower, "~") {
		return true
	}
	return false
}
