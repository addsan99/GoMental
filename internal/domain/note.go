package domain

import (
	"context"
	"time"
)

type NoteID string

type NotePath string

type Tag string

type FileVersion struct {
	ModifiedAt time.Time
	Size       int64
}

type OKFDocument struct {
	Raw string
}

type Note struct {
	ID         NoteID
	Path       NotePath
	Document   OKFDocument
	ModifiedAt time.Time
	Version    FileVersion
}

type NoteSummary struct {
	ID         NoteID
	Title      string
	Path       NotePath
	Type       string
	Tags       []Tag
	Favorite   bool
	ModifiedAt time.Time
}

type NoteRepository interface {
	List(ctx context.Context) ([]NoteSummary, error)
	Read(ctx context.Context, id NoteID) (Note, error)
	Save(ctx context.Context, note Note) error
	Delete(ctx context.Context, id NoteID) error
}
