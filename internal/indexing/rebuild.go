package indexing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/okf"
	"GoMental/internal/search"
	"GoMental/internal/workspace"
)

type ProgressStage string

const (
	ProgressScanning ProgressStage = "scanning"
	ProgressParsing  ProgressStage = "parsing"
	ProgressIndexing ProgressStage = "indexing"
	ProgressGraph    ProgressStage = "graph"
	ProgressComplete ProgressStage = "complete"
)

type RebuildProgress struct {
	Stage     ProgressStage
	Completed int
	Total     int
	NoteID    domain.NoteID
	Message   string
}

type RebuildResult struct {
	WorkspaceRoot string
	TotalNotes    int
	ParsedNotes   int
	FailedNotes   int
	SearchPath    string
	GraphPath     string
	StatePath     string
}

type RebuildState struct {
	WorkspaceRoot        string     `json:"workspaceRoot"`
	StartedAt            time.Time  `json:"startedAt"`
	CompletedAt          time.Time  `json:"completedAt"`
	SoftLinksCompletedAt *time.Time `json:"softLinksCompletedAt,omitempty"`
	SearchSchemaVersion  int        `json:"searchSchemaVersion"`
	TotalNotes           int        `json:"totalNotes"`
	ParsedNotes          int        `json:"parsedNotes"`
	FailedNotes          int        `json:"failedNotes"`
	SearchPath           string     `json:"searchPath"`
	GraphPath            string     `json:"graphPath"`
}

type Rebuilder struct {
	WorkerCount int
	Progress    func(RebuildProgress)
	Now         func() time.Time
}

func (r Rebuilder) Rebuild(ctx context.Context, root string) (RebuildResult, error) {
	result, corpus, err := r.RebuildCore(ctx, root)
	if err != nil {
		return RebuildResult{}, err
	}
	if err := r.RebuildSoftLinks(ctx, result.StatePath, result.GraphPath, corpus); err != nil {
		return RebuildResult{}, err
	}
	return result, nil
}

func (r Rebuilder) RebuildWorkspaceSoftLinks(ctx context.Context, root string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.WorkerCount <= 0 {
		r.WorkerCount = runtime.NumCPU()
	}
	if r.WorkerCount > 8 {
		r.WorkerCount = 8
	}

	ws, err := workspace.Open(root)
	if err != nil {
		return err
	}
	repo := workspace.NewFileNoteRepository(ws)
	r.report(RebuildProgress{Stage: ProgressScanning, Message: "Scanning OKF notes"})
	summaries, err := repo.List(ctx)
	if err != nil {
		return err
	}
	r.report(RebuildProgress{Stage: ProgressScanning, Completed: len(summaries), Total: len(summaries)})
	parsed, _, err := r.parseNotes(ctx, repo, summaries)
	if err != nil {
		return err
	}
	ids := make([]domain.NoteID, 0, len(parsed))
	for _, note := range parsed {
		ids = append(ids, note.ID)
	}
	resolver := okf.NewResolver(ids)
	for i := range parsed {
		parsed[i].Links = resolver.ResolveLinks(parsed[i].ID, parsed[i].Links)
	}
	return r.RebuildSoftLinks(ctx, filepath.Join(ws.MetadataPath(), "state", "rebuild.json"), graph.GraphPath(ws.Root()), parsed)
}

func (r Rebuilder) RebuildCore(ctx context.Context, root string) (RebuildResult, []domain.ParsedOKFNote, error) {
	if err := ctx.Err(); err != nil {
		return RebuildResult{}, nil, err
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.WorkerCount <= 0 {
		r.WorkerCount = runtime.NumCPU()
	}
	if r.WorkerCount > 8 {
		r.WorkerCount = 8
	}

	ws, err := workspace.Open(root)
	if err != nil {
		return RebuildResult{}, nil, err
	}
	repo := workspace.NewFileNoteRepository(ws)
	startedAt := r.Now().UTC()

	r.report(RebuildProgress{Stage: ProgressScanning, Message: "Scanning OKF notes"})
	summaries, err := repo.List(ctx)
	if err != nil {
		return RebuildResult{}, nil, err
	}
	r.report(RebuildProgress{Stage: ProgressScanning, Completed: len(summaries), Total: len(summaries)})

	parsed, failures, err := r.parseNotes(ctx, repo, summaries)
	if err != nil {
		return RebuildResult{}, nil, err
	}
	ids := make([]domain.NoteID, 0, len(parsed))
	for _, note := range parsed {
		ids = append(ids, note.ID)
	}
	resolver := okf.NewResolver(ids)
	for i := range parsed {
		parsed[i].Links = resolver.ResolveLinks(parsed[i].ID, parsed[i].Links)
	}

	searchPath := search.WorkspaceSearchPath(ws.Root())
	graphPath := graph.GraphPath(ws.Root())
	statePath := filepath.Join(ws.MetadataPath(), "state", "rebuild.json")

	if err := os.RemoveAll(searchPath); err != nil {
		return RebuildResult{}, nil, err
	}
	if err := os.RemoveAll(filepath.Dir(graphPath)); err != nil {
		return RebuildResult{}, nil, err
	}

	searchIndex, err := search.OpenBleveIndex(searchPath)
	if err != nil {
		return RebuildResult{}, nil, err
	}
	defer searchIndex.Close()
	graphStore, err := graph.OpenSQLiteStore(graphPath)
	if err != nil {
		return RebuildResult{}, nil, err
	}
	defer graphStore.Close()

	parsedByID := make(map[domain.NoteID]domain.ParsedOKFNote, len(parsed))
	for _, note := range parsed {
		parsedByID[note.ID] = note
	}
	docs, err := searchDocuments(ctx, repo, summaries, parsedByID)
	if err != nil {
		return RebuildResult{}, nil, err
	}
	docsByID := make(map[domain.NoteID]domain.SearchDocument, len(docs))
	for _, doc := range docs {
		docsByID[doc.ID] = doc
	}
	r.report(RebuildProgress{Stage: ProgressIndexing, Total: len(docs), Message: "Rebuilding search index"})
	if err := searchIndex.Rebuild(ctx, docs); err != nil {
		return RebuildResult{}, nil, err
	}
	r.report(RebuildProgress{Stage: ProgressIndexing, Completed: len(docs), Total: len(docs)})

	r.report(RebuildProgress{Stage: ProgressGraph, Total: len(summaries), Message: "Rebuilding graph projection"})
	// Project a note-metadata row for every scanned note (including ones that failed
	// to parse) so the SQLite-backed note list stays complete; attach links and tags
	// for the ones that parsed.
	projections := make([]graph.LinkProjection, 0, len(summaries))
	for i, summary := range summaries {
		if err := ctx.Err(); err != nil {
			return RebuildResult{}, nil, err
		}
		projection := graph.LinkProjection{
			Source: summary.ID,
			Meta:   graph.NoteMeta{ID: summary.ID, Title: summary.Title, Path: string(summary.Path), ModifiedAt: summary.ModifiedAt},
		}
		if note, ok := parsedByID[summary.ID]; ok {
			projection.Hard = noteLinks(note)
			projection.Metadata = graph.MetadataMemberships(note)
			projection.Meta.Tags = note.Tags
			projection.Meta.Type = note.Metadata.Type
			projection.Meta.Favorite = note.Metadata.Favorite
		} else if doc, ok := docsByID[summary.ID]; ok {
			projection.Meta.Tags = doc.Tags
			projection.Meta.Favorite = doc.Favorite
		}
		projections = append(projections, projection)
		r.report(RebuildProgress{Stage: ProgressGraph, Completed: i + 1, Total: len(summaries), NoteID: summary.ID})
	}
	if err := graphStore.ReplaceAllLinks(ctx, projections); err != nil {
		return RebuildResult{}, nil, err
	}

	result := RebuildResult{WorkspaceRoot: ws.Root(), TotalNotes: len(summaries), ParsedNotes: len(parsed), FailedNotes: len(failures), SearchPath: searchPath, GraphPath: graphPath, StatePath: statePath}
	state := RebuildState{WorkspaceRoot: ws.Root(), StartedAt: startedAt, CompletedAt: r.Now().UTC(), SearchSchemaVersion: search.SearchSchemaVersion, TotalNotes: result.TotalNotes, ParsedNotes: result.ParsedNotes, FailedNotes: result.FailedNotes, SearchPath: searchPath, GraphPath: graphPath}
	if err := writeState(statePath, state); err != nil {
		return RebuildResult{}, nil, err
	}
	r.report(RebuildProgress{Stage: ProgressComplete, Completed: len(parsed), Total: len(summaries), Message: "Rebuild complete"})
	return result, parsed, nil
}

func (r Rebuilder) RebuildSoftLinks(ctx context.Context, statePath string, graphPath string, parsed []domain.ParsedOKFNote) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.WorkerCount <= 0 {
		r.WorkerCount = runtime.NumCPU()
	}
	if r.WorkerCount > 8 {
		r.WorkerCount = 8
	}
	store, err := graph.OpenSQLiteStore(graphPath)
	if err != nil {
		return err
	}
	defer store.Close()
	r.report(RebuildProgress{Stage: ProgressGraph, Total: len(parsed), Message: "Rebuilding inferred links"})
	projections, err := r.softLinkProjections(ctx, parsed)
	if err != nil {
		return err
	}
	if err := store.ReplaceAllInferredLinks(ctx, projections); err != nil {
		return err
	}
	state, err := ReadState(statePath)
	if err != nil {
		return err
	}
	completedAt := r.Now().UTC()
	state.SoftLinksCompletedAt = &completedAt
	if err := writeState(statePath, state); err != nil {
		return err
	}
	r.report(RebuildProgress{Stage: ProgressComplete, Completed: len(parsed), Total: len(parsed), Message: "Inferred links complete"})
	return nil
}

func (r Rebuilder) parseNotes(ctx context.Context, repo *workspace.FileNoteRepository, summaries []domain.NoteSummary) ([]domain.ParsedOKFNote, []error, error) {
	jobs := make(chan domain.NoteSummary)
	results := make(chan parseResult)
	workers := r.WorkerCount
	if workers > len(summaries) && len(summaries) > 0 {
		workers = len(summaries)
	}
	if workers == 0 {
		workers = 1
	}
	var wg sync.WaitGroup
	codec := okf.NewCodec()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for summary := range jobs {
				if err := ctx.Err(); err != nil {
					results <- parseResult{err: err, fatal: true}
					continue
				}
				note, err := repo.Read(ctx, summary.ID)
				if err != nil {
					results <- parseResult{id: summary.ID, err: err}
					continue
				}
				parsed, err := codec.Decode(note.ID, note.Document.Raw, note.ModifiedAt)
				if err != nil {
					results <- parseResult{id: note.ID, err: err}
					continue
				}
				results <- parseResult{id: note.ID, note: parsed}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, summary := range summaries {
			select {
			case <-ctx.Done():
				return
			case jobs <- summary:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	parsed := make([]domain.ParsedOKFNote, 0, len(summaries))
	var failures []error
	completed := 0
	for result := range results {
		if result.fatal {
			return nil, nil, result.err
		}
		completed++
		if result.err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", result.id, result.err))
		} else {
			parsed = append(parsed, result.note)
		}
		r.report(RebuildProgress{Stage: ProgressParsing, Completed: completed, Total: len(summaries), NoteID: result.id})
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return parsed, failures, nil
}

func searchDocuments(ctx context.Context, repo *workspace.FileNoteRepository, summaries []domain.NoteSummary, parsedByID map[domain.NoteID]domain.ParsedOKFNote) ([]domain.SearchDocument, error) {
	docs := make([]domain.SearchDocument, 0, len(summaries))
	for _, summary := range summaries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if parsed, ok := parsedByID[summary.ID]; ok {
			docs = append(docs, domain.SearchDocumentFromParsed(parsed, summary.Path))
			continue
		}
		note, err := repo.Read(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		docs = append(docs, okf.SearchDocumentFromRaw(note.ID, note.Path, note.Document.Raw, note.ModifiedAt))
	}
	return docs, nil
}

func (r Rebuilder) softLinkProjections(ctx context.Context, parsed []domain.ParsedOKFNote) ([]graph.LinkProjection, error) {
	idx := graph.BuildCorpusIndex(parsed)
	inference := graph.NewLocalInferenceService(graph.InferenceConfig{})
	all, err := inference.InferAll(ctx, idx)
	if err != nil {
		return nil, err
	}
	projections := make([]graph.LinkProjection, len(parsed))
	for i, note := range parsed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		projections[i] = graph.LinkProjection{Source: note.ID, Soft: all[note.ID]}
		r.report(RebuildProgress{Stage: ProgressGraph, Completed: i + 1, Total: len(parsed), NoteID: note.ID})
	}
	return projections, nil
}

func (r Rebuilder) report(progress RebuildProgress) {
	if r.Progress != nil {
		r.Progress(progress)
	}
}

type parseResult struct {
	id    domain.NoteID
	note  domain.ParsedOKFNote
	err   error
	fatal bool
}

func noteLinks(note domain.ParsedOKFNote) []domain.NoteLink {
	links := make([]domain.NoteLink, 0, len(note.Links))
	for _, link := range note.Links {
		links = append(links, domain.NoteLink{Source: note.ID, Target: link.RawTarget, ResolvedID: link.ResolvedID, DisplayText: link.DisplayText, Heading: link.Heading, Strength: domain.LinkStrengthHard})
	}
	return links
}

func writeState(path string, state RebuildState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func ReadState(path string) (RebuildState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RebuildState{}, err
	}
	var state RebuildState
	if err := json.Unmarshal(data, &state); err != nil {
		return RebuildState{}, err
	}
	return state, nil
}

func IsMissingState(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
