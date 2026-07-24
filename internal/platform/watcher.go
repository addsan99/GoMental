package platform

import (
	"context"
	"reflect"
	"sort"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/workspace"
)

type WorkspaceChangeSet struct {
	Changed []domain.NoteID
	Deleted []domain.NoteID
}

type WatchOptions struct {
	Interval time.Duration
}

type WorkspaceWatcher struct {
	workspace workspace.Workspace
	options   WatchOptions
}

func NewWorkspaceWatcher(workspace workspace.Workspace, options WatchOptions) *WorkspaceWatcher {
	if options.Interval <= 0 {
		options.Interval = 750 * time.Millisecond
	}
	return &WorkspaceWatcher{workspace: workspace, options: options}
}

func (w *WorkspaceWatcher) Run(ctx context.Context, emit func(WorkspaceChangeSet)) error {
	previous, err := w.snapshot(ctx)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(w.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current, err := w.snapshot(ctx)
			if err != nil {
				continue
			}
			changes := diffSnapshots(previous, current)
			previous = current
			if len(changes.Changed) > 0 || len(changes.Deleted) > 0 {
				emit(changes)
			}
		}
	}
}

type fileSnapshot struct {
	ModifiedAt time.Time
	Size       int64
}

func (w *WorkspaceWatcher) snapshot(ctx context.Context) (map[domain.NoteID]fileSnapshot, error) {
	notes, err := w.workspace.ScanNotes(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[domain.NoteID]fileSnapshot, len(notes))
	for _, note := range notes {
		out[note.ID] = fileSnapshot{ModifiedAt: note.ModifiedAt, Size: note.Size}
	}
	return out, nil
}

func diffSnapshots(previous, current map[domain.NoteID]fileSnapshot) WorkspaceChangeSet {
	var changes WorkspaceChangeSet
	for id, version := range current {
		if old, ok := previous[id]; !ok || !reflect.DeepEqual(old, version) {
			changes.Changed = append(changes.Changed, id)
		}
	}
	for id := range previous {
		if _, ok := current[id]; !ok {
			changes.Deleted = append(changes.Deleted, id)
		}
	}
	sort.Slice(changes.Changed, func(i, j int) bool { return changes.Changed[i] < changes.Changed[j] })
	sort.Slice(changes.Deleted, func(i, j int) bool { return changes.Deleted[i] < changes.Deleted[j] })
	return changes
}
