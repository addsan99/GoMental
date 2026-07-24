package graph

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"GoMental/internal/domain"
)

func TestTitleMatcherMatchesBruteForce(t *testing.T) {
	alphabet := []byte("abc go-λ ")
	rng := rand.New(rand.NewSource(1))
	randString := func(maxLen int) string {
		n := rng.Intn(maxLen + 1)
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(b)
	}
	for iter := 0; iter < 500; iter++ {
		patterns := map[string][]domain.NoteID{}
		for p := 0; p < rng.Intn(6); p++ {
			pat := randString(5)
			if pat == "" {
				continue
			}
			patterns[pat] = append(patterns[pat], domain.NoteID(fmt.Sprintf("n%d-%d", iter, p)))
		}
		matcher := newTitleMatcher(patterns)
		text := randString(30)
		got := matcher.FindNoteIDs(text)
		want := bruteForceFindNoteIDs(patterns, text)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iter %d text=%q patterns=%v\n got=%v\nwant=%v", iter, text, patterns, got, want)
		}
	}
}

func TestTitleMatcherSubstringAndDuplicateTitles(t *testing.T) {
	patterns := map[string][]domain.NoteID{
		"go":     {"golang-note"},
		"golang": {"golang-note", "another"},
	}
	matcher := newTitleMatcher(patterns)
	got := matcher.FindNoteIDs("learning golang today")
	// "go" is a substring of "golang", so both patterns match.
	want := map[domain.NoteID]struct{}{"golang-note": {}, "another": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	if len(matcher.FindNoteIDs("nothing here")) != 0 {
		t.Fatal("expected no matches")
	}
}
