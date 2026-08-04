package domain

import "context"

type SearchDocument struct {
	ID          NoteID
	Path        NotePath
	Title       string
	Body        string
	Headings    []string
	Tags        []Tag
	Aliases     []string
	LinkTargets []string
	Favorite    bool
	ModifiedAt  int64
}

type SearchQuery struct {
	Text          string
	Tags          []Tag
	PathPrefix    string
	FavoritesOnly bool
	Limit         int
	Cursor        string
}

type SearchResult struct {
	ID        NoteID
	Path      NotePath
	Title     string
	Score     float64
	Fragments []string
	Favorite  bool
}

type SearchIndex interface {
	Index(ctx context.Context, document SearchDocument) error
	Delete(ctx context.Context, id NoteID) error
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	Rebuild(ctx context.Context, documents []SearchDocument) error
	Close() error
}

func SearchDocumentFromParsed(note ParsedOKFNote, path NotePath) SearchDocument {
	headings := make([]string, len(note.Headings))
	for i, heading := range note.Headings {
		headings[i] = heading.Text
	}
	links := make([]string, len(note.Links))
	for i, link := range note.Links {
		if link.ResolvedID != nil {
			links[i] = string(*link.ResolvedID)
		} else {
			links[i] = link.RawTarget
		}
	}
	return SearchDocument{
		ID:          note.ID,
		Path:        path,
		Title:       note.Title,
		Body:        note.PlainText,
		Headings:    headings,
		Tags:        note.Tags,
		LinkTargets: links,
		Favorite:    note.Metadata.Favorite,
		ModifiedAt:  note.ModifiedAt.Unix(),
	}
}
