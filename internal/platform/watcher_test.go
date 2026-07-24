package platform

import (
	"testing"
	"time"

	"GoMental/internal/domain"
)

func TestDiffSnapshotsCoalescesCreateModifyDelete(t *testing.T) {
	oldTime := time.Unix(10, 0)
	newTime := time.Unix(20, 0)
	previous := map[domain.NoteID]fileSnapshot{
		"alpha": {ModifiedAt: oldTime, Size: 10},
		"beta":  {ModifiedAt: oldTime, Size: 20},
	}
	current := map[domain.NoteID]fileSnapshot{
		"alpha": {ModifiedAt: newTime, Size: 10},
		"gamma": {ModifiedAt: newTime, Size: 30},
	}

	changes := diffSnapshots(previous, current)

	if got, want := changes.Changed, []domain.NoteID{"alpha", "gamma"}; !sameIDs(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	if got, want := changes.Deleted, []domain.NoteID{"beta"}; !sameIDs(got, want) {
		t.Fatalf("deleted = %v, want %v", got, want)
	}
}

func TestDiffSnapshotsIgnoresUnchangedFiles(t *testing.T) {
	version := fileSnapshot{ModifiedAt: time.Unix(10, 0), Size: 10}
	changes := diffSnapshots(map[domain.NoteID]fileSnapshot{"alpha": version}, map[domain.NoteID]fileSnapshot{"alpha": version})
	if len(changes.Changed) != 0 || len(changes.Deleted) != 0 {
		t.Fatalf("changes = %+v, want none", changes)
	}
}

func sameIDs(left, right []domain.NoteID) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
