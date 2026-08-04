package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/graph"
	"GoMental/internal/importers"
	"GoMental/internal/indexing"
	"GoMental/internal/okf"
	"GoMental/internal/platform"
	"GoMental/internal/search"
	"GoMental/internal/workspace"
)

type EventSink func(name string, payload any)

const (
	ErrWorkspaceInaccessible = "workspace.inaccessible"
	ErrOKFDecodeFailed       = "okf.decode_failed"
	ErrSearchCorrupt         = "search.corrupt"
	ErrGraphCorrupt          = "graph.corrupt"
	ErrExternalConflict      = "edit.external_conflict"
	ErrBrokenRename          = "note.rename_broken"
	ErrNoteExists            = "note.exists"
	ErrProjectionMissing     = "projection.missing"
	ErrProjectionStale       = "projection.stale"
	ErrProjectionRepair      = "projection.repair_failed"
)

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e AppError) Error() string {
	if e.Detail == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Message + ": " + e.Detail
}

type WorkspaceDTO struct {
	Root      string `json:"root"`
	NoteCount int    `json:"noteCount"`
}

type NoteSummaryDTO struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Path       string   `json:"path"`
	Type       string   `json:"type"`
	Tags       []string `json:"tags"`
	Favorite   bool     `json:"favorite"`
	ModifiedAt string   `json:"modifiedAt"`
}

type NoteDTO struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Content    string `json:"content"`
	Favorite   bool   `json:"favorite"`
	ModifiedAt string `json:"modifiedAt"`
	// Version is an opaque optimistic-concurrency token derived from the file
	// version (modtime+size). Clients round-trip it via SaveNoteRequest.BaseVersion.
	Version string `json:"version"`
}

type SaveNoteRequest struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	// BaseVersion is the opaque version token the client last read (from
	// NoteDTO.Version). When set (and Force is false), the save is rejected with
	// ErrExternalConflict if the note changed on disk since then. Empty =
	// unconditional last-write-wins (the single-user desktop default).
	BaseVersion string `json:"baseVersion,omitempty"`
	// Force bypasses the version check even when BaseVersion is set.
	Force bool `json:"force,omitempty"`
}

type MoveNoteRequest struct {
	ID    string `json:"id"`
	NewID string `json:"newId"`
}

type ImportURLRequest struct {
	URL string `json:"url"`
}

// CreateNoteRequest drives the agent-ergonomic create endpoint.
type CreateNoteRequest struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	// Mode selects collision behavior: "create" (default; fail if the id exists),
	// "upsert" (write regardless), or "unique" (auto-suffix the id to a free one).
	Mode string `json:"mode"`
}

type SaveNoteAssetRequest struct {
	NoteID     string `json:"noteId"`
	FileName   string `json:"fileName"`
	MIMEType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
}

type SaveNoteAssetResponse struct {
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
}

type NoteAssetRequest struct {
	NoteID string `json:"noteId"`
	Path   string `json:"path"`
}

type SearchQueryDTO struct {
	Text          string   `json:"text"`
	Tags          []string `json:"tags"`
	PathPrefix    string   `json:"pathPrefix"`
	FavoritesOnly bool     `json:"favoritesOnly"`
	Limit         int      `json:"limit"`
}

type SearchResultDTO struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Score     float64  `json:"score"`
	Fragments []string `json:"fragments"`
	Favorite  bool     `json:"favorite"`
}

type GraphFilterDTO struct {
	PathPrefix           string   `json:"pathPrefix"`
	Tags                 []string `json:"tags"`
	FavoritesOnly        bool     `json:"favoritesOnly"`
	IncludeUnresolved    bool     `json:"includeUnresolved"`
	IncludeSoftLinks     bool     `json:"includeSoftLinks"`
	IncludeMetadataLinks bool     `json:"includeMetadataLinks"`
	Depth                int      `json:"depth"`
}

// GraphQueryDTO is the unified graph-selection request (see domain.GraphQuery).
// Seed empty means a full-graph query; when set it is the focus note for a
// depth-bounded neighborhood. Types/Tags/PathPrefix restrict the note node set.
type GraphQueryDTO struct {
	Seed                 string   `json:"seed"`
	Depth                int      `json:"depth"`
	Types                []string `json:"types"`
	Tags                 []string `json:"tags"`
	PathPrefix           string   `json:"pathPrefix"`
	FavoritesOnly        bool     `json:"favoritesOnly"`
	IncludeSoftLinks     bool     `json:"includeSoftLinks"`
	IncludeMetadataLinks bool     `json:"includeMetadataLinks"`
	IncludeUnresolved    bool     `json:"includeUnresolved"`
}

// ListNotesQueryDTO parameterizes a paginated note list. Limit 0 means "all".
type ListNotesQueryDTO struct {
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	SortBy        string `json:"sortBy"`
	Desc          bool   `json:"desc"`
	Tag           string `json:"tag"`
	Type          string `json:"type"`
	Search        string `json:"search"`
	FavoritesOnly bool   `json:"favoritesOnly"`
}

// NotesPageDTO is a page of note summaries plus the total matching the filter.
type NotesPageDTO struct {
	Items  []NoteSummaryDTO `json:"items"`
	Total  int              `json:"total"`
	Offset int              `json:"offset"`
	Limit  int              `json:"limit"`
}

type GraphDTO struct {
	Nodes []GraphNodeDTO `json:"nodes"`
	Edges []GraphEdgeDTO `json:"edges"`
}

type GraphNodeDTO struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	NoteID string `json:"noteId,omitempty"`
}

type GraphEdgeDTO struct {
	ID     string  `json:"id"`
	Source string  `json:"source"`
	Target string  `json:"target"`
	Kind   string  `json:"kind"`
	Score  float64 `json:"score,omitempty"`
}

type LayoutCoordinatesDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type LayoutSnapshotDTO struct {
	Coordinates map[string]LayoutCoordinatesDTO `json:"coordinates"`
	UpdatedAt   string                          `json:"updatedAt"`
}

type NoteLinkDTO struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	ResolvedID  string `json:"resolvedId,omitempty"`
	DisplayText string `json:"displayText"`
	Heading     string `json:"heading"`
	Strength    string `json:"strength"`
}

// EvidenceDTO is one signal explaining why two notes relate.
type EvidenceDTO struct {
	Kind   string  `json:"kind"`
	Detail string  `json:"detail"`
	Weight float64 `json:"weight"`
}

// LinkExplanationDTO is the explain_link result: whether two notes are related,
// whether the link is a persisted hard link, the combined score, the evidence
// list, and a one-line human summary.
type LinkExplanationDTO struct {
	Source   string        `json:"source"`
	Target   string        `json:"target"`
	Related  bool          `json:"related"`
	HardLink bool          `json:"hardLink"`
	Score    float64       `json:"score"`
	Evidence []EvidenceDTO `json:"evidence"`
	Summary  string        `json:"summary"`
}

// ContextNoteDTO is one neighbor returned by expand_context.
type ContextNoteDTO struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Excerpt  string `json:"excerpt"`
	Relation string `json:"relation"`
}

// ExpandContextDTO is a note plus the content of its graph neighborhood, so an
// agent can gather connected context in a single call.
type ExpandContextDTO struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Content   string           `json:"content"`
	Version   string           `json:"version"`
	Depth     int              `json:"depth"`
	Neighbors []ContextNoteDTO `json:"neighbors"`
}

type RebuildResultDTO struct {
	WorkspaceRoot string `json:"workspaceRoot"`
	TotalNotes    int    `json:"totalNotes"`
	ParsedNotes   int    `json:"parsedNotes"`
	FailedNotes   int    `json:"failedNotes"`
	SearchPath    string `json:"searchPath"`
	GraphPath     string `json:"graphPath"`
	StatePath     string `json:"statePath"`
}

type RecentWorkspaceDTO struct {
	Path     string `json:"path"`
	OpenedAt string `json:"openedAt"`
}

type UIState map[string]any

type Settings struct {
	Version    int                          `json:"version"`
	Appearance AppearanceSettings           `json:"appearance"`
	NoteView   NoteViewSettings             `json:"noteView"`
	GraphView  GraphViewSettings            `json:"graphView"`
	Workspaces map[string]WorkspaceSettings `json:"workspaces"`
}

type AppearanceSettings struct {
	Theme string `json:"theme"`
}

type NoteViewSettings struct {
	DefaultEditMode string `json:"defaultEditMode"`
	ShowFindBar     bool   `json:"showFindBar"`
}

type GraphViewSettings struct {
	DefaultMode  string `json:"defaultMode"`
	DefaultDepth int    `json:"defaultDepth"`
}

type WorkspaceSettings struct {
	DefaultType  string   `json:"defaultType"`
	EnabledTypes []string `json:"enabledTypes"`
	AccessMode   string   `json:"accessMode"`
	GitURL       string   `json:"gitUrl,omitempty"`
}

type Service struct {
	mu           sync.Mutex
	events       EventSink
	recentStore  workspace.RecentWorkspaceStore
	statePath    string
	settingsPath string
	workspace    workspace.Workspace
	repo         *workspace.FileNoteRepository
	searchIndex  *search.BleveIndex
	graphStore   *graph.SQLiteStore
	corpus       *liveCorpus
	// listFromSQLite is set at open time when the SQLite note-metadata projection is
	// complete (one listable row per note). When false (e.g. a just-upgraded DB whose
	// meta columns are still empty), ListNotes falls back to the filesystem so it is
	// never fooled by a partially populated projection.
	listFromSQLite bool
	watchCancel    context.CancelFunc
	watchDone      chan struct{}
	softCancel     context.CancelFunc
	softDone       chan struct{}
	// corpusCancel/corpusDone govern the background goroutine that builds the full
	// parsed corpus index after OpenWorkspace returns (the open path installs a
	// cheap ID-only index first). Torn down alongside the inference worker.
	corpusCancel context.CancelFunc
	corpusDone   chan struct{}
	inference    *inferenceWorker
	// noteLocks serializes concurrent writes to the SAME note (keyed by
	// normalized NoteID) while allowing writes to different notes to proceed in
	// parallel. Values are *sync.Mutex.
	noteLocks sync.Map
}

func NewService(events EventSink) (*Service, error) {
	recent, err := workspace.DefaultRecentWorkspaceStore()
	if err != nil {
		return nil, err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return NewServiceWithStores(events, recent, filepath.Join(configDir, "GoMental", "ui-state.json")), nil
}

func NewServiceWithStores(events EventSink, recentStore workspace.RecentWorkspaceStore, statePath string) *Service {
	return &Service{
		events:       events,
		recentStore:  recentStore,
		statePath:    statePath,
		settingsPath: filepath.Join(filepath.Dir(statePath), "GoMental.Settings.json"),
	}
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopSoftLinkRebuildLocked()
	s.stopWatcherLocked()
	return s.closeStores()
}

func (s *Service) OpenWorkspace(ctx context.Context, root string) (WorkspaceDTO, error) {
	ws, err := workspace.Open(root)
	if err != nil {
		return WorkspaceDTO{}, appErr(ErrWorkspaceInaccessible, "Could not open workspace", err)
	}
	repo := workspace.NewFileNoteRepository(ws)
	notes, err := repo.List(ctx)
	if err != nil {
		return WorkspaceDTO{}, appErr("workspace.list_failed", "Could not list workspace notes", err)
	}
	s.mu.Lock()
	s.stopSoftLinkRebuildLocked()
	s.stopWatcherLocked()
	_ = s.closeStores()
	s.mu.Unlock()
	if reason, err := projectionHealth(ctx, ws, notes); err != nil {
		return WorkspaceDTO{}, err
	} else if reason != "" {
		if err := s.repairWorkspaceProjections(ctx, ws.Root(), reason, nil); err != nil {
			return WorkspaceDTO{}, err
		}
	}
	searchIndex, searchErr := search.OpenBleveIndex(search.WorkspaceSearchPath(ws.Root()))
	graphStore, graphErr := graph.OpenSQLiteStore(graph.GraphPath(ws.Root()))
	if searchErr != nil || graphErr != nil {
		if searchIndex != nil {
			_ = searchIndex.Close()
		}
		if graphStore != nil {
			_ = graphStore.Close()
		}
		if err := s.repairWorkspaceProjections(ctx, ws.Root(), projectionOpenFailureReason(searchErr, graphErr), errors.Join(searchErr, graphErr)); err != nil {
			return WorkspaceDTO{}, err
		}
		searchIndex, searchErr = search.OpenBleveIndex(search.WorkspaceSearchPath(ws.Root()))
		if searchErr != nil {
			return WorkspaceDTO{}, appErr(ErrSearchCorrupt, "Search index is corrupt and could not be reopened after repair", searchErr)
		}
		graphStore, graphErr = graph.OpenSQLiteStore(graph.GraphPath(ws.Root()))
		if graphErr != nil {
			_ = searchIndex.Close()
			return WorkspaceDTO{}, appErr(ErrGraphCorrupt, "Graph store is corrupt and could not be reopened after repair", graphErr)
		}
	}
	if err := s.recentStore.Add(ctx, ws.Root()); err != nil {
		_ = searchIndex.Close()
		_ = graphStore.Close()
		return WorkspaceDTO{}, appErr("workspace.recent_failed", "Could not update recent workspaces", err)
	}

	// Install a cheap ID-only corpus index immediately so OpenWorkspace returns
	// without parsing every note off disk. Link resolution on the save hot path
	// only needs the ID set, so it works against this index; full per-note
	// metadata is filled in by the background build below (and any external edits
	// are reconciled by the watcher). This keeps time-to-notes independent of
	// workspace size.
	idOnly := make([]domain.ParsedOKFNote, len(notes))
	for i, n := range notes {
		idOnly[i] = domain.ParsedOKFNote{ID: n.ID}
	}
	corpusIndex := graph.BuildCorpusIndex(idOnly)

	// The SQLite note list is authoritative only when its metadata is complete —
	// i.e. every scanned note has a listable row. A freshly rebuilt/created DB
	// satisfies this; a pre-migration DB with empty meta columns does not, so we
	// serve the list from the filesystem until the next rebuild repopulates it.
	listable, countErr := graphStore.CountNotes(ctx)
	listFromSQLite := countErr == nil && listable == len(notes)

	s.mu.Lock()
	s.stopSoftLinkRebuildLocked()
	s.stopWatcherLocked()
	_ = s.closeStores()
	s.workspace = ws
	s.repo = repo
	s.searchIndex = searchIndex
	s.graphStore = graphStore
	s.listFromSQLite = listFromSQLite
	s.corpus = newLiveCorpus(corpusIndex)
	s.startWatcherLocked(ws)
	s.startInferenceWorkerLocked()
	s.startCorpusBuildLocked(ws, repo, notes)
	s.mu.Unlock()

	dto := WorkspaceDTO{Root: ws.Root(), NoteCount: len(notes)}
	s.emit("workspace:loaded", dto)
	return dto, nil
}

// startCorpusBuildLocked launches the background goroutine that parses the full
// corpus and swaps it into the live index installed by OpenWorkspace, then kicks
// the initial soft-link recompute against the now-complete corpus. Must be called
// with s.mu held and after s.corpus / s.inference are set. It captures the corpus
// and worker pointers (not the Service fields) so a subsequent reopen — which
// replaces both — leaves this build to finish harmlessly against the old, now
// unreferenced session; the cancel simply avoids wasted work.
func (s *Service) startCorpusBuildLocked(ws workspace.Workspace, repo *workspace.FileNoteRepository, notes []domain.NoteSummary) {
	s.stopCorpusBuildLocked()
	corpus := s.corpus
	worker := s.inference
	if corpus == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.corpusCancel = cancel
	s.corpusDone = done
	go func() {
		defer close(done)
		// The goroutine touches only concurrency-safe surfaces (liveCorpus,
		// inferenceWorker, emit) and never s.mu, so stopCorpusBuildLocked can hold
		// s.mu while waiting on done without deadlocking.
		if parsed, err := parseCorpus(ctx, repo); err == nil && ctx.Err() == nil {
			corpus.Replace(graph.BuildCorpusIndex(parsed))
		}
		if ctx.Err() != nil {
			return
		}
		if worker != nil && softLinksNeedRebuild(ctx, ws, notes) {
			worker.MarkDirty() // kick an initial full soft-link recompute
		}
		s.emit("corpus:ready", WorkspaceDTO{Root: ws.Root(), NoteCount: len(notes)})
	}()
}

// ListNotes returns every note's summary, served from the SQLite note-metadata
// projection (an indexed table scan) rather than a filesystem tree walk — so it
// is cheap to call on every note:updated/note:deleted event. It falls back to the
// filesystem when the projection is empty or unavailable (e.g. a just-upgraded DB
// before its first rebuild) so the list is never spuriously empty.
func (s *Service) ListNotes(ctx context.Context) ([]NoteSummaryDTO, error) {
	repo, graphStore, fromSQLite, err := s.listSnapshot()
	if err != nil {
		return nil, err
	}
	if fromSQLite && graphStore != nil {
		if res, lerr := graphStore.ListNotes(ctx, graph.ListNotesOptions{}); lerr == nil {
			return noteRowsToDTOs(res.Items), nil
		}
	}
	notes, err := repo.List(ctx)
	if err != nil {
		return nil, appErr("notes.list_failed", "Could not list notes", err)
	}
	out := make([]NoteSummaryDTO, len(notes))
	for i, note := range notes {
		out[i] = noteSummaryDTO(note)
	}
	return out, nil
}

// ListNotesPage serves a sorted/filtered/paginated page of note summaries from
// SQLite. Used by clients that page a large workspace instead of loading it whole.
func (s *Service) ListNotesPage(ctx context.Context, q ListNotesQueryDTO) (NotesPageDTO, error) {
	_, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NotesPageDTO{}, err
	}
	res, err := graphStore.ListNotes(ctx, graph.ListNotesOptions{
		Offset:        q.Offset,
		Limit:         q.Limit,
		SortBy:        q.SortBy,
		Desc:          q.Desc,
		Tag:           q.Tag,
		Type:          q.Type,
		Search:        q.Search,
		FavoritesOnly: q.FavoritesOnly,
	})
	if err != nil {
		return NotesPageDTO{}, appErr("notes.list_failed", "Could not list notes", err)
	}
	return NotesPageDTO{Items: noteRowsToDTOs(res.Items), Total: res.Total, Offset: q.Offset, Limit: q.Limit}, nil
}

func (s *Service) ReadNote(ctx context.Context, id string) (NoteDTO, error) {
	repo, err := s.repoSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	note, err := repo.Read(ctx, domain.NoteID(id))
	if err != nil {
		return NoteDTO{}, appErr("notes.read_failed", "Could not read note", err)
	}
	return noteDTO(note), nil
}

func (s *Service) SaveNote(ctx context.Context, req SaveNoteRequest) (NoteDTO, error) {
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	noteID, err := ws.NormalizeNoteID(req.ID)
	if err != nil {
		return NoteDTO{}, appErr("notes.invalid_id", "Invalid note id", err)
	}
	// Serialize concurrent writes to the same note; different notes stay parallel.
	unlock := s.lockNote(noteID)
	defer unlock()

	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	note := domain.Note{ID: noteID, Document: domain.OKFDocument{Raw: req.Content}}

	if req.BaseVersion != "" && !req.Force {
		expected, verr := decodeVersion(req.BaseVersion)
		if verr != nil {
			return NoteDTO{}, appErr("edit.invalid_version", "Invalid base version token", verr)
		}
		if err := repo.SaveIfUnchanged(ctx, note, expected); err != nil {
			if errors.Is(err, workspace.ErrVersionConflict) {
				detail := ""
				if current, rerr := repo.Read(ctx, noteID); rerr == nil {
					detail = encodeVersion(current.Version)
				}
				return NoteDTO{}, AppError{Code: ErrExternalConflict, Message: "Note changed on disk since it was read", Detail: detail}
			}
			return NoteDTO{}, appErr("notes.save_failed", "Could not save note", err)
		}
	} else if err := repo.Save(ctx, note); err != nil {
		return NoteDTO{}, appErr("notes.save_failed", "Could not save note", err)
	}

	read, err := repo.Read(ctx, noteID)
	if err != nil {
		return NoteDTO{}, appErr("notes.read_failed", "Could not read saved note", err)
	}
	// Fast path: keep only the edited note's hard-link + search projection on the
	// save hot path (no full-corpus reparse). Soft-link inference is recomputed
	// off the hot path via the background rebuild machinery.
	if err := updateOneProjectionFast(ctx, repo, searchIndex, graphStore, s.corpusState(), read); err != nil {
		return NoteDTO{}, projectionUpdateErr(err)
	}
	dto := noteDTO(read)
	s.emit("note:updated", dto)
	s.markDirty(read.ID)
	return dto, nil
}

func (s *Service) SetNoteFavorite(ctx context.Context, id string, favorite bool) (NoteDTO, error) {
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	noteID, err := ws.NormalizeNoteID(id)
	if err != nil {
		return NoteDTO{}, appErr("notes.invalid_id", "Invalid note id", err)
	}
	unlock := s.lockNote(noteID)
	defer unlock()

	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	note, err := repo.Read(ctx, noteID)
	if err != nil {
		return NoteDTO{}, appErr("notes.read_failed", "Could not read note", err)
	}
	if next, changed := setFavoriteFrontmatter(note.Document.Raw, favorite); changed {
		note.Document.Raw = next
		if err := repo.Save(ctx, note); err != nil {
			return NoteDTO{}, appErr("notes.save_failed", "Could not save favorite state", err)
		}
		note, err = repo.Read(ctx, noteID)
		if err != nil {
			return NoteDTO{}, appErr("notes.read_failed", "Could not read updated note", err)
		}
		if err := updateOneProjectionFast(ctx, repo, searchIndex, graphStore, s.corpusState(), note); err != nil {
			var decodeErr domain.DecodeError
			if !errors.As(err, &decodeErr) {
				return NoteDTO{}, projectionUpdateErr(err)
			}
			if err := graphStore.UpsertNoteMeta(ctx, graph.NoteMeta{
				ID:         note.ID,
				Title:      workspace.TitleFromID(note.ID),
				Path:       string(note.Path),
				ModifiedAt: note.ModifiedAt,
				Favorite:   favorite,
			}); err != nil {
				return NoteDTO{}, projectionUpdateErr(err)
			}
		}
	}
	dto := noteDTO(note)
	s.emit("note:updated", dto)
	s.emit("graph:updated", map[string]any{"changed": []string{dto.ID}})
	s.markDirty(note.ID)
	return dto, nil
}

// CreateNote creates a note with agent-friendly collision semantics (see
// CreateNoteRequest.Mode). It shares the per-note lock and fast projection path
// with SaveNote.
func (s *Service) CreateNote(ctx context.Context, req CreateNoteRequest) (NoteDTO, error) {
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return NoteDTO{}, appErr("notes.invalid_id", "Note id is required", nil)
	}
	noteID, err := ws.NormalizeNoteID(req.ID)
	if err != nil {
		return NoteDTO{}, appErr("notes.invalid_id", "Invalid note id", err)
	}
	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	mode := req.Mode
	if mode == "" {
		mode = "create"
	}
	if mode == "unique" {
		free, uerr := uniqueImportedNoteID(ctx, repo, noteID)
		if uerr != nil {
			return NoteDTO{}, appErr("notes.id_failed", "Could not choose a unique note id", uerr)
		}
		noteID = free
	}

	unlock := s.lockNote(noteID)
	defer unlock()

	if mode == "create" {
		if _, rerr := repo.Read(ctx, noteID); rerr == nil {
			return NoteDTO{}, AppError{Code: ErrNoteExists, Message: "Note already exists", Detail: string(noteID)}
		}
	}
	note := domain.Note{ID: noteID, Document: domain.OKFDocument{Raw: req.Content}}
	if err := repo.Save(ctx, note); err != nil {
		return NoteDTO{}, appErr("notes.save_failed", "Could not save note", err)
	}
	read, err := repo.Read(ctx, noteID)
	if err != nil {
		return NoteDTO{}, appErr("notes.read_failed", "Could not read created note", err)
	}
	if err := updateOneProjectionFast(ctx, repo, searchIndex, graphStore, s.corpusState(), read); err != nil {
		return NoteDTO{}, projectionUpdateErr(err)
	}
	dto := noteDTO(read)
	s.emit("note:updated", dto)
	s.markDirty(read.ID)
	return dto, nil
}

func (s *Service) ImportURL(ctx context.Context, req ImportURLRequest) (NoteDTO, error) {
	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	sourceURL, err := normalizeImportURL(req.URL)
	if err != nil {
		return NoteDTO{}, appErr("import.invalid_url", "Enter a valid http or https URL", err)
	}
	contentType, html, err := fetchImportURL(ctx, sourceURL)
	if err != nil {
		return NoteDTO{}, appErr("import.fetch_failed", "Could not fetch import URL", err)
	}
	result, err := importers.DefaultRegistry().Import(ctx, importers.ImportSource{
		URL:         sourceURL,
		ContentType: contentType,
		HTML:        html,
	})
	if err != nil {
		if importers.IsNoImporter(err) {
			return NoteDTO{}, appErr("import.unsupported", "No importer could handle this URL", err)
		}
		return NoteDTO{}, appErr("import.failed", "Could not import URL", err)
	}
	result.Document.ID, err = uniqueImportedNoteID(ctx, repo, result.Document.ID)
	if err != nil {
		return NoteDTO{}, appErr("import.note_id_failed", "Could not choose an import note ID", err)
	}
	if err := repo.Save(ctx, result.Document); err != nil {
		return NoteDTO{}, appErr("import.save_failed", "Could not save imported note", err)
	}
	read, err := repo.Read(ctx, result.Document.ID)
	if err != nil {
		return NoteDTO{}, appErr("notes.read_failed", "Could not read imported note", err)
	}
	if err := updateOneProjection(ctx, repo, searchIndex, graphStore, s.corpusState(), read); err != nil {
		return NoteDTO{}, projectionUpdateErr(err)
	}
	dto := noteDTO(read)
	s.emit("note:updated", dto)
	s.emit("graph:updated", map[string]any{"changed": []string{dto.ID}})
	s.markDirty(read.ID)
	return dto, nil
}

func (s *Service) SaveNoteAsset(ctx context.Context, req SaveNoteAssetRequest) (SaveNoteAssetResponse, error) {
	select {
	case <-ctx.Done():
		return SaveNoteAssetResponse{}, ctx.Err()
	default:
	}
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return SaveNoteAssetResponse{}, err
	}
	noteID, err := ws.NormalizeNoteID(req.NoteID)
	if err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.invalid_note", "Invalid note for asset", err)
	}
	data, err := decodeAssetBase64(req.DataBase64)
	if err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.decode_failed", "Could not decode pasted image", err)
	}
	if len(data) == 0 {
		return SaveNoteAssetResponse{}, appErr("asset.empty", "Image data is empty", nil)
	}
	if len(data) > 25*1024*1024 {
		return SaveNoteAssetResponse{}, appErr("asset.too_large", "Image is larger than 25 MB", nil)
	}
	mimeType := supportedImageMIME(req.MIMEType, data)
	if mimeType == "" {
		return SaveNoteAssetResponse{}, appErr("asset.unsupported_type", "Unsupported image type", fmt.Errorf("%s", req.MIMEType))
	}
	fileName := uniqueAssetFileName(req.FileName, mimeType)
	assetDir := filepath.Join(ws.Root(), "assets", filepath.FromSlash(string(noteID)))
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.mkdir_failed", "Could not create asset folder", err)
	}
	assetPath := filepath.Join(assetDir, fileName)
	for i := 1; ; i++ {
		if _, err := os.Stat(assetPath); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return SaveNoteAssetResponse{}, appErr("asset.stat_failed", "Could not inspect asset path", err)
		}
		ext := filepath.Ext(fileName)
		base := strings.TrimSuffix(fileName, ext)
		assetPath = filepath.Join(assetDir, fmt.Sprintf("%s-%d%s", base, i, ext))
	}
	if err := ensurePathInside(ws.Root(), assetPath); err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.path_escape", "Asset path escapes workspace", err)
	}
	if err := os.WriteFile(assetPath, data, 0o644); err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.write_failed", "Could not save image asset", err)
	}
	notePath, err := ws.PathForNoteID(noteID)
	if err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.note_path_failed", "Could not resolve note path", err)
	}
	rel, err := filepath.Rel(filepath.Dir(notePath), assetPath)
	if err != nil {
		return SaveNoteAssetResponse{}, appErr("asset.relative_failed", "Could not create relative asset path", err)
	}
	slashed := filepath.ToSlash(rel)
	return SaveNoteAssetResponse{Path: slashed, Markdown: fmt.Sprintf("![%s](%s)", markdownAltText(fileName), slashed)}, nil
}

func (s *Service) LoadNoteAssetDataURL(ctx context.Context, req NoteAssetRequest) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return "", err
	}
	noteID, err := ws.NormalizeNoteID(req.NoteID)
	if err != nil {
		return "", appErr("asset.invalid_note", "Invalid note for asset", err)
	}
	assetPath, err := resolveNoteAssetPath(ws, noteID, req.Path)
	if err != nil {
		return "", appErr("asset.invalid_path", "Invalid image path", err)
	}
	data, err := os.ReadFile(assetPath)
	if err != nil {
		return "", appErr("asset.read_failed", "Could not read image asset", err)
	}
	mimeType := supportedImageMIME("", data)
	if mimeType == "" {
		return "", appErr("asset.unsupported_type", "Unsupported image type", nil)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (s *Service) DeleteNote(ctx context.Context, id string) error {
	noteID := domain.NoteID(id)
	if ws, wsErr := s.workspaceSnapshot(); wsErr == nil {
		if normalized, nErr := ws.NormalizeNoteID(id); nErr == nil {
			noteID = normalized
		}
	}
	unlock := s.lockNote(noteID)
	defer unlock()
	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return err
	}
	if err := repo.Delete(ctx, noteID); err != nil {
		return appErr("notes.delete_failed", "Could not delete note", err)
	}
	if err := searchIndex.Delete(ctx, noteID); err != nil {
		return appErr("search.delete_failed", "Could not delete search entry", err)
	}
	if err := graphStore.DeleteNote(ctx, noteID); err != nil {
		return appErr("graph.delete_failed", "Could not delete graph entry", err)
	}
	if corpus := s.corpusState(); corpus != nil {
		corpus.Delete(noteID)
	}
	s.markDeleted(noteID)
	s.emit("note:deleted", map[string]string{"id": id})
	return nil
}

func (s *Service) MoveNote(ctx context.Context, req MoveNoteRequest) (NoteDTO, error) {
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	oldID, err := ws.NormalizeNoteID(req.ID)
	if err != nil {
		return NoteDTO{}, appErr("notes.invalid_id", "Invalid source note id", err)
	}
	newID, err := ws.NormalizeNoteID(req.NewID)
	if err != nil {
		return NoteDTO{}, appErr("notes.invalid_id", "Invalid target note id", err)
	}
	if oldID == newID {
		return s.ReadNote(ctx, string(newID))
	}

	first, second := oldID, newID
	if string(second) < string(first) {
		first, second = second, first
	}
	unlockFirst := s.lockNote(first)
	defer unlockFirst()
	unlockSecond := s.lockNote(second)
	defer unlockSecond()

	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	if err := repo.Rename(ctx, oldID, newID); err != nil {
		return NoteDTO{}, appErr("notes.move_failed", "Could not move note", err)
	}
	read, err := repo.Read(ctx, newID)
	if err != nil {
		return NoteDTO{}, appErr("notes.read_failed", "Could not read moved note", err)
	}
	if rewritten := rewriteMovedImageLinks(ws, oldID, newID, read.Document.Raw); rewritten != read.Document.Raw {
		read.Document.Raw = rewritten
		if err := repo.Save(ctx, read); err != nil {
			return NoteDTO{}, appErr("notes.move_rewrite_failed", "Could not update moved note image links", err)
		}
		read, err = repo.Read(ctx, newID)
		if err != nil {
			return NoteDTO{}, appErr("notes.read_failed", "Could not read moved note", err)
		}
	}
	if err := updateIncrementalProjections(ctx, repo, searchIndex, graphStore, s.corpusState(), []domain.NoteID{newID}, []domain.NoteID{oldID}); err != nil {
		return NoteDTO{}, projectionUpdateErr(err)
	}
	dto := noteDTO(read)
	s.emit("note:deleted", map[string]string{"id": string(oldID)})
	s.emit("note:updated", dto)
	s.emit("graph:updated", map[string]any{"changed": []string{string(newID)}, "deleted": []string{string(oldID)}})
	s.markDeleted(oldID)
	s.markDirty(newID)
	return dto, nil
}

func (s *Service) Search(ctx context.Context, input SearchQueryDTO) ([]SearchResultDTO, error) {
	_, searchIndex, _, err := s.sessionSnapshot()
	if err != nil {
		return nil, err
	}
	results, err := searchIndex.Search(ctx, domain.SearchQuery{Text: input.Text, Tags: tags(input.Tags), PathPrefix: input.PathPrefix, FavoritesOnly: input.FavoritesOnly, Limit: input.Limit})
	if err != nil {
		return nil, appErr("search.failed", "Search failed", err)
	}
	for i := range results {
		if results[i].Favorite {
			results[i].Score *= 1.08
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	out := make([]SearchResultDTO, len(results))
	for i, result := range results {
		out[i] = SearchResultDTO{ID: string(result.ID), Path: string(result.Path), Title: result.Title, Score: result.Score, Fragments: result.Fragments, Favorite: result.Favorite}
	}
	return out, nil
}

func (s *Service) FullGraph(ctx context.Context, input GraphFilterDTO) (GraphDTO, error) {
	_, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return GraphDTO{}, err
	}
	g, err := graphStore.FullGraph(ctx, domain.GraphFilter{Tags: tags(input.Tags), PathPrefix: input.PathPrefix, FavoritesOnly: input.FavoritesOnly, IncludeUnresolved: input.IncludeUnresolved, IncludeSoftLinks: input.IncludeSoftLinks, IncludeMetadataLinks: input.IncludeMetadataLinks})
	if err != nil {
		return GraphDTO{}, appErr("graph.failed", "Could not load graph", err)
	}
	return graphDTO(g), nil
}

func (s *Service) Neighborhood(ctx context.Context, id string, depth int) (GraphDTO, error) {
	_, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return GraphDTO{}, err
	}
	g, err := graphStore.Neighborhood(ctx, domain.NoteID(id), depth)
	if err != nil {
		return GraphDTO{}, appErr("graph.neighborhood_failed", "Could not load local graph", err)
	}
	return graphDTO(g), nil
}

// GraphQuery is the unified selection endpoint that backs the reworked graph
// view: an optional seed + depth combined with metadata predicates (type / tag /
// path). Neighborhood and FullGraph remain as the legacy presets.
func (s *Service) GraphQuery(ctx context.Context, input GraphQueryDTO) (GraphDTO, error) {
	_, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return GraphDTO{}, err
	}
	var seed *domain.NoteID
	if input.Seed != "" {
		v := domain.NoteID(input.Seed)
		seed = &v
	}
	g, err := graphStore.Query(ctx, domain.GraphQuery{
		Seed:                 seed,
		Depth:                input.Depth,
		Types:                input.Types,
		Tags:                 tags(input.Tags),
		PathPrefix:           input.PathPrefix,
		FavoritesOnly:        input.FavoritesOnly,
		IncludeSoftLinks:     input.IncludeSoftLinks,
		IncludeMetadataLinks: input.IncludeMetadataLinks,
		IncludeUnresolved:    input.IncludeUnresolved,
	})
	if err != nil {
		return GraphDTO{}, appErr("graph.query_failed", "Could not query graph", err)
	}
	return graphDTO(g), nil
}

func (s *Service) LoadGraphLayout(ctx context.Context) (LayoutSnapshotDTO, error) {
	root, err := s.workspaceRootSnapshot()
	if err != nil {
		return LayoutSnapshotDTO{}, err
	}
	if err := ctx.Err(); err != nil {
		return LayoutSnapshotDTO{}, err
	}
	data, err := os.ReadFile(graphLayoutPath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LayoutSnapshotDTO{Coordinates: map[string]LayoutCoordinatesDTO{}}, nil
		}
		return LayoutSnapshotDTO{}, appErr("layout.read_failed", "Could not read graph layout", err)
	}
	var snapshot LayoutSnapshotDTO
	if err := jsonUnmarshal(data, &snapshot); err != nil {
		return LayoutSnapshotDTO{}, appErr("layout.decode_failed", "Could not decode graph layout", err)
	}
	if snapshot.Coordinates == nil {
		snapshot.Coordinates = map[string]LayoutCoordinatesDTO{}
	}
	return snapshot, nil
}

func (s *Service) SaveGraphLayout(ctx context.Context, snapshot LayoutSnapshotDTO) error {
	root, err := s.workspaceRootSnapshot()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot.Coordinates == nil {
		snapshot.Coordinates = map[string]LayoutCoordinatesDTO{}
	}
	if snapshot.UpdatedAt == "" {
		snapshot.UpdatedAt = time.Now().UTC().Format(timeFormat)
	}
	path := graphLayoutPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return appErr("layout.write_failed", "Could not create graph layout directory", err)
	}
	data, err := jsonMarshalIndent(snapshot)
	if err != nil {
		return appErr("layout.encode_failed", "Could not encode graph layout", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return appErr("layout.write_failed", "Could not write graph layout", err)
	}
	return nil
}
func (s *Service) Backlinks(ctx context.Context, id string) ([]NoteLinkDTO, error) {
	_, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return nil, err
	}
	links, err := graphStore.Backlinks(ctx, domain.NoteID(id))
	if err != nil {
		return nil, appErr("graph.backlinks_failed", "Could not load backlinks", err)
	}
	out := make([]NoteLinkDTO, len(links))
	for i, link := range links {
		out[i] = noteLinkDTO(link)
	}
	return out, nil
}

// ExplainLink reports why source and target relate: content-derived evidence
// (title mentions, shared tag/type/heading) from graph.ExplainRelation plus a
// hard-link check against the persisted graph. It powers the explain_link tool.
func (s *Service) ExplainLink(ctx context.Context, sourceID, targetID string) (LinkExplanationDTO, error) {
	repo, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return LinkExplanationDTO{}, err
	}
	src, err := readParsed(ctx, repo, sourceID)
	if err != nil {
		return LinkExplanationDTO{}, err
	}
	tgt, err := readParsed(ctx, repo, targetID)
	if err != nil {
		return LinkExplanationDTO{}, err
	}
	expl := graph.ExplainRelation(src, tgt)

	links, err := graphStore.OutgoingLinks(ctx, src.ID)
	if err != nil {
		return LinkExplanationDTO{}, appErr("graph.outgoing_failed", "Could not load outgoing links", err)
	}
	for _, l := range links {
		if l.ResolvedID != nil && *l.ResolvedID == tgt.ID {
			expl.HardLink = true
			break
		}
	}
	expl.Related = expl.HardLink || len(expl.Evidence) > 0
	return linkExplanationDTO(expl), nil
}

const (
	expandMaxNeighbors = 25
	expandExcerptRunes = 500
)

// ExpandContext returns a note plus the content of its graph neighborhood in a
// single call: the focus note (full content + version) and, for each neighboring
// note up to the given depth, a title, excerpt, and the connecting relation kind.
func (s *Service) ExpandContext(ctx context.Context, id string, depth int) (ExpandContextDTO, error) {
	if depth <= 0 {
		depth = 1
	}
	repo, _, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return ExpandContextDTO{}, err
	}
	focus, err := repo.Read(ctx, domain.NoteID(id))
	if err != nil {
		return ExpandContextDTO{}, appErr("notes.read_failed", "Could not read note", err)
	}
	focusParsed, err := okf.NewCodec().Decode(focus.ID, focus.Document.Raw, focus.ModifiedAt)
	if err != nil {
		return ExpandContextDTO{}, appErr(ErrOKFDecodeFailed, "Could not parse note", err)
	}
	g, err := graphStore.Neighborhood(ctx, domain.NoteID(id), depth)
	if err != nil {
		return ExpandContextDTO{}, appErr("graph.neighborhood_failed", "Could not load local graph", err)
	}

	// Locate the focus node id so we can classify the connecting edge kind.
	focusNodeID := string(focus.ID)
	for _, n := range g.Nodes {
		if n.NoteID != nil && *n.NoteID == focus.ID {
			focusNodeID = n.ID
			break
		}
	}

	dto := ExpandContextDTO{
		ID:      string(focus.ID),
		Title:   focusParsed.Title,
		Content: focus.Document.Raw,
		Version: encodeVersion(focus.Version),
		Depth:   depth,
	}
	for _, n := range g.Nodes {
		if len(dto.Neighbors) >= expandMaxNeighbors {
			break
		}
		if n.Kind != domain.GraphNodeNote || n.NoteID == nil || *n.NoteID == focus.ID {
			continue
		}
		neighbor, rerr := repo.Read(ctx, *n.NoteID)
		if rerr != nil {
			continue // skip an unreadable neighbor rather than fail the whole call
		}
		excerpt := ""
		title := n.Label // graph label falls back to the id; prefer the parsed title
		if parsed, derr := okf.NewCodec().Decode(neighbor.ID, neighbor.Document.Raw, neighbor.ModifiedAt); derr == nil {
			excerpt = truncateRunes(parsed.PlainText, expandExcerptRunes)
			if parsed.Title != "" {
				title = parsed.Title
			}
		}
		dto.Neighbors = append(dto.Neighbors, ContextNoteDTO{
			ID:       string(*n.NoteID),
			Title:    title,
			Excerpt:  excerpt,
			Relation: edgeRelation(g.Edges, focusNodeID, n.ID),
		})
	}
	return dto, nil
}

// readParsed reads and decodes a single note into a ParsedOKFNote.
func readParsed(ctx context.Context, repo *workspace.FileNoteRepository, id string) (domain.ParsedOKFNote, error) {
	note, err := repo.Read(ctx, domain.NoteID(id))
	if err != nil {
		return domain.ParsedOKFNote{}, appErr("notes.read_failed", "Could not read note", err)
	}
	parsed, err := okf.NewCodec().Decode(note.ID, note.Document.Raw, note.ModifiedAt)
	if err != nil {
		return domain.ParsedOKFNote{}, appErr(ErrOKFDecodeFailed, "Could not parse note", err)
	}
	return parsed, nil
}

func linkExplanationDTO(e domain.LinkExplanation) LinkExplanationDTO {
	evidence := make([]EvidenceDTO, len(e.Evidence))
	for i, ev := range e.Evidence {
		evidence[i] = EvidenceDTO{Kind: string(ev.Kind), Detail: ev.Detail, Weight: ev.Weight}
	}
	return LinkExplanationDTO{
		Source:   string(e.Source),
		Target:   string(e.Target),
		Related:  e.Related,
		HardLink: e.HardLink,
		Score:    e.Score,
		Evidence: evidence,
		Summary:  explanationSummary(e),
	}
}

func explanationSummary(e domain.LinkExplanation) string {
	if !e.Related {
		return "No direct or inferred relationship found."
	}
	var parts []string
	if e.HardLink {
		parts = append(parts, "a hard link")
	}
	kinds := make(map[domain.EvidenceKind]int)
	for _, ev := range e.Evidence {
		kinds[ev.Kind]++
	}
	if kinds[domain.EvidenceTitleMention] > 0 {
		parts = append(parts, "title mention")
	}
	if kinds[domain.EvidenceSharedTag] > 0 {
		parts = append(parts, "shared tag(s)")
	}
	if kinds[domain.EvidenceSharedType] > 0 {
		parts = append(parts, "shared type")
	}
	if kinds[domain.EvidenceSharedHeading] > 0 {
		parts = append(parts, "shared heading(s)")
	}
	return "Related via " + strings.Join(parts, ", ") + "."
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

func edgeRelation(edges []domain.GraphEdge, a, b string) string {
	for _, e := range edges {
		if (e.Source == a && e.Target == b) || (e.Source == b && e.Target == a) {
			return string(e.Kind)
		}
	}
	return ""
}

func (s *Service) Rebuild(ctx context.Context) (RebuildResultDTO, error) {
	s.mu.Lock()
	root := s.workspace.Root()
	if root != "" {
		s.stopSoftLinkRebuildLocked()
		s.stopWatcherLocked()
		_ = s.closeStores()
	}
	s.mu.Unlock()
	if root == "" {
		return RebuildResultDTO{}, workspaceNotOpenErr()
	}
	rebuilder := indexing.Rebuilder{Progress: func(progress indexing.RebuildProgress) { s.emit("index:progress", progress) }}
	result, _, err := rebuilder.RebuildCore(ctx, root)
	if err != nil {
		return RebuildResultDTO{}, appErr("rebuild.failed", "Could not rebuild workspace projections", err)
	}
	if _, err := s.OpenWorkspace(ctx, root); err != nil {
		return RebuildResultDTO{}, err
	}
	dto := rebuildDTO(result)
	s.emit("graph:updated", dto)
	return dto, nil
}

func (s *Service) RecentWorkspaces(ctx context.Context) ([]RecentWorkspaceDTO, error) {
	items, err := s.recentStore.List(ctx)
	if err != nil {
		return nil, appErr("workspace.recent_failed", "Could not read recent workspaces", err)
	}
	out := make([]RecentWorkspaceDTO, len(items))
	for i, item := range items {
		out[i] = RecentWorkspaceDTO{Path: item.Path, OpenedAt: item.OpenedAt.Format(timeFormat)}
	}
	return out, nil
}

func (s *Service) LoadSettings(ctx context.Context) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return defaultSettings(), err
	}
	settings := defaultSettings()
	data, err := os.ReadFile(s.settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settings, nil
		}
		return defaultSettings(), appErr("settings.read_failed", "Could not read settings", err)
	}
	if err := jsonUnmarshal(data, &settings); err != nil {
		return defaultSettings(), appErr("settings.decode_failed", "Could not decode settings", err)
	}
	return normalizeSettings(settings), nil
}

func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	settings = normalizeSettings(settings)
	if err := os.MkdirAll(filepath.Dir(s.settingsPath), 0o755); err != nil {
		return appErr("settings.write_failed", "Could not create settings directory", err)
	}
	data, err := jsonMarshalIndent(settings)
	if err != nil {
		return appErr("settings.encode_failed", "Could not encode settings", err)
	}
	if err := os.WriteFile(s.settingsPath, append(data, '\n'), 0o644); err != nil {
		return appErr("settings.write_failed", "Could not write settings", err)
	}
	return nil
}

func (s *Service) LoadUIState(ctx context.Context) (UIState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UIState{}, nil
		}
		return nil, appErr("state.read_failed", "Could not read UI state", err)
	}
	var state UIState
	if err := jsonUnmarshal(data, &state); err != nil {
		return nil, appErr("state.decode_failed", "Could not decode UI state", err)
	}
	return state, nil
}

func (s *Service) SaveUIState(ctx context.Context, state UIState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return appErr("state.write_failed", "Could not create UI state directory", err)
	}
	data, err := jsonMarshalIndent(state)
	if err != nil {
		return appErr("state.encode_failed", "Could not encode UI state", err)
	}
	if err := os.WriteFile(s.statePath, append(data, '\n'), 0o644); err != nil {
		return appErr("state.write_failed", "Could not write UI state", err)
	}
	return nil
}

// IsWorkspaceOpen reports whether a workspace is currently open (cheap; no scan).
func (s *Service) IsWorkspaceOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repo != nil && s.workspace.Root() != ""
}

func (s *Service) workspaceRootSnapshot() (string, error) {
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return "", err
	}
	return ws.Root(), nil
}

func (s *Service) workspaceSnapshot() (workspace.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspace.Root() == "" {
		return workspace.Workspace{}, workspaceNotOpenErr()
	}
	return s.workspace, nil
}

func decodeAssetBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if comma := strings.Index(value, ","); strings.HasPrefix(value, "data:") && comma >= 0 {
		value = value[comma+1:]
	}
	return base64.StdEncoding.DecodeString(value)
}

func supportedImageMIME(hint string, data []byte) string {
	hint = strings.ToLower(strings.TrimSpace(strings.Split(hint, ";")[0]))
	switch hint {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml":
		return hint
	}
	detected := http.DetectContentType(data)
	switch detected {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return detected
	}
	head := strings.TrimPrefix(strings.TrimSpace(string(data)), "\ufeff")
	if strings.HasPrefix(head, "<svg") {
		return "image/svg+xml"
	}
	// SVG documents often begin with an XML prolog (and/or a DOCTYPE) before the
	// <svg> root element, so a bare "<svg" prefix check misses them.
	if strings.HasPrefix(head, "<?xml") || strings.HasPrefix(head, "<!DOCTYPE") {
		if strings.Contains(head, "<svg") {
			return "image/svg+xml"
		}
	}
	return ""
}

func uniqueAssetFileName(name string, mimeType string) string {
	base := sanitizeAssetName(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if base == "" || base == "." {
		base = "image"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !assetExtensionMatchesMIME(ext, mimeType) {
		ext = extensionForMIME(mimeType)
	}
	stamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s-%s%s", stamp, base, ext)
}

func sanitizeAssetName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	lastDash := false
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func assetExtensionMatchesMIME(ext string, mimeType string) bool {
	switch mimeType {
	case "image/png":
		return ext == ".png"
	case "image/jpeg":
		return ext == ".jpg" || ext == ".jpeg"
	case "image/gif":
		return ext == ".gif"
	case "image/webp":
		return ext == ".webp"
	case "image/svg+xml":
		return ext == ".svg"
	}
	return false
}

func extensionForMIME(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}

func markdownAltText(fileName string) string {
	base := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.TrimSpace(base)
	if base == "" {
		return "image"
	}
	return base
}

var (
	markdownImageLinkPattern = regexp.MustCompile(`\\?!\[([^\]]*)\]\(([^)]+)\)`)
	htmlDoubleSrcPattern     = regexp.MustCompile(`\bsrc="([^"]+)"`)
	htmlSingleSrcPattern     = regexp.MustCompile(`\bsrc='([^']+)'`)
)

func rewriteMovedImageLinks(ws workspace.Workspace, oldID domain.NoteID, newID domain.NoteID, content string) string {
	content = markdownImageLinkPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := markdownImageLinkPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		next, ok := movedImageSource(ws, oldID, newID, parts[2])
		if !ok {
			return match
		}
		bang := "!"
		if strings.HasPrefix(match, `\!`) {
			bang = `\!`
		}
		return fmt.Sprintf("%s[%s](%s)", bang, parts[1], next)
	})
	content = htmlDoubleSrcPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := htmlDoubleSrcPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		next, ok := movedImageSource(ws, oldID, newID, parts[1])
		if !ok {
			return match
		}
		return `src="` + next + `"`
	})
	content = htmlSingleSrcPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := htmlSingleSrcPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		next, ok := movedImageSource(ws, oldID, newID, parts[1])
		if !ok {
			return match
		}
		return `src='` + next + `'`
	})
	return content
}

func movedImageSource(ws workspace.Workspace, oldID domain.NoteID, newID domain.NoteID, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isRemoteImageSource(raw) {
		return raw, false
	}
	pathPart, suffix := splitImageSourceSuffix(raw)
	if pathPart == "" {
		return raw, false
	}
	oldNotePath, err := ws.PathForNoteID(oldID)
	if err != nil {
		return raw, false
	}
	newNotePath, err := ws.PathForNoteID(newID)
	if err != nil {
		return raw, false
	}
	var target string
	switch {
	case strings.HasPrefix(pathPart, "/"):
		target = filepath.Join(ws.Root(), filepath.FromSlash(strings.TrimPrefix(pathPart, "/")))
	case strings.HasPrefix(pathPart, `\`):
		target = filepath.Join(ws.Root(), filepath.FromSlash(strings.TrimLeft(strings.ReplaceAll(pathPart, `\`, "/"), "/")))
	default:
		target = filepath.Join(filepath.Dir(oldNotePath), filepath.FromSlash(strings.ReplaceAll(pathPart, `\`, "/")))
	}
	target = filepath.Clean(target)
	if err := ensurePathInside(ws.Root(), target); err != nil {
		return raw, false
	}
	rel, err := filepath.Rel(filepath.Dir(newNotePath), target)
	if err != nil {
		return raw, false
	}
	return filepath.ToSlash(rel) + suffix, true
}

func isRemoteImageSource(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if lower == "" || strings.HasPrefix(lower, "#") || strings.HasPrefix(lower, "//") {
		return true
	}
	if idx := strings.Index(lower, ":"); idx > 0 {
		prefix := lower[:idx]
		if !strings.ContainsAny(prefix, `/\`) {
			return true
		}
	}
	return false
}

func splitImageSourceSuffix(raw string) (string, string) {
	for i, r := range raw {
		if r == '?' || r == '#' {
			return raw[:i], raw[i:]
		}
	}
	return raw, ""
}

func resolveNoteAssetPath(ws workspace.Workspace, noteID domain.NoteID, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return "", workspace.ErrPathEscapesWorkspace
	}
	notePath, err := ws.PathForNoteID(noteID)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(filepath.Dir(notePath), filepath.FromSlash(strings.ReplaceAll(raw, `\`, "/")))
	clean := filepath.Clean(joined)
	if err := ensurePathInside(ws.Root(), clean); err != nil {
		return "", err
	}
	return clean, nil
}

func normalizeImportURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("missing host")
	}
	return parsed.String(), nil
}

func fetchImportURL(ctx context.Context, sourceURL string) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "GoMental/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	const maxImportBytes = 8 * 1024 * 1024
	body, err := readLimited(resp.Body, maxImportBytes)
	if err != nil {
		return "", nil, err
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	return contentType, body, nil
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func uniqueImportedNoteID(ctx context.Context, repo *workspace.FileNoteRepository, base domain.NoteID) (domain.NoteID, error) {
	summaries, err := repo.List(ctx)
	if err != nil {
		return "", err
	}
	existing := map[string]struct{}{}
	for _, summary := range summaries {
		existing[strings.ToLower(string(summary.ID))] = struct{}{}
	}
	if _, ok := existing[strings.ToLower(string(base))]; !ok {
		return base, nil
	}
	for i := 2; ; i++ {
		candidate := domain.NoteID(fmt.Sprintf("%s-%d", base, i))
		if _, ok := existing[strings.ToLower(string(candidate))]; !ok {
			return candidate, nil
		}
	}
}

func ensurePathInside(root string, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, filepath.Clean(abs))
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return workspace.ErrPathEscapesWorkspace
	}
	return nil
}

func graphLayoutPath(root string) string {
	return filepath.Join(root, ".workspace", "layout", "graph-layout.json")
}

func defaultSettings() Settings {
	return Settings{
		Version: 1,
		Appearance: AppearanceSettings{
			Theme: "dark",
		},
		NoteView: NoteViewSettings{
			DefaultEditMode: "rich",
			ShowFindBar:     true,
		},
		GraphView: GraphViewSettings{
			DefaultMode:  "2d",
			DefaultDepth: 2,
		},
		Workspaces: map[string]WorkspaceSettings{},
	}
}

func normalizeSettings(settings Settings) Settings {
	defaults := defaultSettings()
	if settings.Version <= 0 {
		settings.Version = defaults.Version
	}
	if settings.Appearance.Theme != "light" && settings.Appearance.Theme != "dark" {
		settings.Appearance.Theme = defaults.Appearance.Theme
	}
	if settings.NoteView.DefaultEditMode != "source" && settings.NoteView.DefaultEditMode != "rich" {
		settings.NoteView.DefaultEditMode = defaults.NoteView.DefaultEditMode
	}
	if settings.GraphView.DefaultMode != "3d" && settings.GraphView.DefaultMode != "2d" {
		settings.GraphView.DefaultMode = defaults.GraphView.DefaultMode
	}
	if settings.GraphView.DefaultDepth < 1 {
		settings.GraphView.DefaultDepth = defaults.GraphView.DefaultDepth
	}
	if settings.GraphView.DefaultDepth > 4 {
		settings.GraphView.DefaultDepth = 4
	}
	if settings.Workspaces == nil {
		settings.Workspaces = map[string]WorkspaceSettings{}
	}
	for path, workspaceSettings := range settings.Workspaces {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath == "" {
			delete(settings.Workspaces, path)
			continue
		}
		normalizedWorkspaceSettings := normalizeWorkspaceSettings(workspaceSettings)
		if trimmedPath != path {
			delete(settings.Workspaces, path)
		}
		settings.Workspaces[trimmedPath] = normalizedWorkspaceSettings
	}
	return settings
}

func normalizeWorkspaceSettings(settings WorkspaceSettings) WorkspaceSettings {
	defaults := defaultWorkspaceSettings()
	settings.DefaultType = strings.TrimSpace(settings.DefaultType)
	if settings.DefaultType == "" {
		settings.DefaultType = defaults.DefaultType
	}
	settings.EnabledTypes = normalizeTypeList(settings.EnabledTypes)
	if len(settings.EnabledTypes) == 0 {
		settings.EnabledTypes = defaults.EnabledTypes
	}
	if !containsString(settings.EnabledTypes, settings.DefaultType) {
		settings.EnabledTypes = append([]string{settings.DefaultType}, settings.EnabledTypes...)
	}
	if settings.AccessMode != "readOnlyLocal" && settings.AccessMode != "readOnlyGit" && settings.AccessMode != "editable" {
		settings.AccessMode = defaults.AccessMode
	}
	settings.GitURL = strings.TrimSpace(settings.GitURL)
	if settings.AccessMode != "readOnlyGit" {
		settings.GitURL = ""
	}
	return settings
}

func defaultWorkspaceSettings() WorkspaceSettings {
	return WorkspaceSettings{
		DefaultType: "concept",
		EnabledTypes: []string{
			"concept",
			"adr",
			"service",
			"entity",
			"how-to",
			"recipe",
			"gotcha",
			"convention",
			"plan",
			"progress",
			"meeting",
		},
		AccessMode: "editable",
	}
}

func normalizeTypeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func (s *Service) repairWorkspaceProjections(ctx context.Context, root string, reason string, cause error) error {
	if reason == "" {
		reason = ErrProjectionStale
	}
	logProjectionRecovery(root, "starting", reason, cause)
	s.emit("projection:repairing", map[string]string{"reason": reason})
	rebuilder := indexing.Rebuilder{Progress: func(progress indexing.RebuildProgress) { s.emit("index:progress", progress) }}
	result, _, err := rebuilder.RebuildCore(ctx, root)
	if err != nil {
		logProjectionRecovery(root, "failed", reason, err)
		return appErr(ErrProjectionRepair, "Could not rebuild workspace projections", err)
	}
	logProjectionRecovery(root, "complete", reason, nil)
	s.emit("projection:repaired", rebuildDTO(result))
	return nil
}

func softLinksNeedRebuild(ctx context.Context, ws workspace.Workspace, notes []domain.NoteSummary) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	state, err := indexing.ReadState(rebuildStatePath(ws))
	if err != nil || state.SoftLinksCompletedAt == nil {
		return true
	}
	for _, note := range notes {
		if note.ModifiedAt.After(*state.SoftLinksCompletedAt) {
			return true
		}
	}
	return false
}

func projectionHealth(ctx context.Context, ws workspace.Workspace, notes []domain.NoteSummary) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	searchPath := search.WorkspaceSearchPath(ws.Root())
	graphPath := graph.GraphPath(ws.Root())
	statePath := rebuildStatePath(ws)
	if _, err := os.Stat(searchPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProjectionMissing, nil
		}
		return "", appErr(ErrProjectionMissing, "Could not inspect search projection", err)
	}
	if _, err := os.Stat(graphPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProjectionMissing, nil
		}
		return "", appErr(ErrProjectionMissing, "Could not inspect graph projection", err)
	}
	state, err := indexing.ReadState(statePath)
	if err != nil {
		if indexing.IsMissingState(err) {
			return ErrProjectionMissing, nil
		}
		return ErrProjectionStale, nil
	}
	if state.TotalNotes != len(notes) || state.ParsedNotes > state.TotalNotes {
		return ErrProjectionStale, nil
	}
	for _, note := range notes {
		if note.ModifiedAt.After(state.CompletedAt) {
			return ErrProjectionStale, nil
		}
	}
	return "", nil
}

func projectionOpenFailureReason(searchErr error, graphErr error) string {
	if searchErr != nil {
		return ErrSearchCorrupt
	}
	if graphErr != nil {
		return ErrGraphCorrupt
	}
	return ErrProjectionStale
}

func rebuildStatePath(ws workspace.Workspace) string {
	return filepath.Join(ws.MetadataPath(), "state", "rebuild.json")
}

func logProjectionRecovery(root string, status string, reason string, err error) {
	if root == "" {
		return
	}
	path := filepath.Join(root, ".workspace", "logs", "GoMental.log")
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return
	}
	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		return
	}
	defer file.Close()
	logger := log.New(file, "", log.LstdFlags|log.LUTC)
	if err != nil {
		logger.Printf("projection_recovery status=%s reason=%s error=%q", status, reason, err.Error())
		return
	}
	logger.Printf("projection_recovery status=%s reason=%s", status, reason)
}
func (s *Service) startWatcherLocked(ws workspace.Workspace) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan platform.WorkspaceChangeSet, 16)
	done := make(chan struct{})
	s.watchCancel = cancel
	s.watchDone = done
	watcher := platform.NewWorkspaceWatcher(ws, platform.WatchOptions{})
	go func() {
		_ = watcher.Run(ctx, func(change platform.WorkspaceChangeSet) {
			select {
			case events <- change:
			default:
				go func() {
					select {
					case events <- change:
					case <-ctx.Done():
					}
				}()
			}
		})
	}()
	go func() {
		defer close(done)
		s.consumeWorkspaceChanges(ctx, events)
	}()
}

const inferenceQuiet = 300 * time.Millisecond

// startInferenceWorkerLocked launches the coalescing soft-link worker for the open
// workspace. It captures the current corpus and graph store; on reopen the caller
// stops the old worker first. Must be called with s.mu held and after s.corpus /
// s.graphStore are set.
func (s *Service) startInferenceWorkerLocked() {
	s.stopSoftLinkRebuildLocked()
	corpus := s.corpus
	store := s.graphStore
	if corpus == nil || store == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	worker := newInferenceWorker(inferenceQuiet, corpus.Snapshot, store, s.emit)
	s.softCancel = cancel
	s.softDone = done
	s.inference = worker
	go func() {
		defer close(done)
		worker.run(ctx)
	}()
}

// stopSoftLinkRebuildLocked cancels the inference worker and the background
// corpus builder, waiting for both to exit. Kept under this name because it is
// invoked from every store-teardown site, so extending it here guarantees the
// corpus builder is torn down on close/reopen without touching each call site.
func (s *Service) stopSoftLinkRebuildLocked() {
	s.stopCorpusBuildLocked()
	if s.softCancel == nil {
		return
	}
	cancel := s.softCancel
	done := s.softDone
	s.softCancel = nil
	s.softDone = nil
	s.inference = nil
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// stopCorpusBuildLocked cancels the background corpus-index build (if running)
// and waits for it to exit. Must be called with s.mu held.
func (s *Service) stopCorpusBuildLocked() {
	if s.corpusCancel == nil {
		return
	}
	cancel := s.corpusCancel
	done := s.corpusDone
	s.corpusCancel = nil
	s.corpusDone = nil
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// markDirty / markDeleted forward note changes to the inference worker (no-op if
// no workspace is open).
func (s *Service) markDirty(ids ...domain.NoteID) {
	s.mu.Lock()
	worker := s.inference
	s.mu.Unlock()
	if worker != nil {
		worker.MarkDirty(ids...)
	}
}

func (s *Service) markDeleted(ids ...domain.NoteID) {
	s.mu.Lock()
	worker := s.inference
	s.mu.Unlock()
	if worker != nil {
		worker.MarkDeleted(ids...)
	}
}

func (s *Service) stopWatcherLocked() {
	if s.watchCancel == nil {
		return
	}
	cancel := s.watchCancel
	done := s.watchDone
	s.watchCancel = nil
	s.watchDone = nil
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Service) consumeWorkspaceChanges(ctx context.Context, events <-chan platform.WorkspaceChangeSet) {
	var pending platform.WorkspaceChangeSet
	var timer *time.Timer
	var timerC <-chan time.Time
	flush := func() {
		if len(pending.Changed) == 0 && len(pending.Deleted) == 0 {
			return
		}
		batch := pending
		pending = platform.WorkspaceChangeSet{}
		processCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := s.processWorkspaceChanges(processCtx, batch); err != nil {
			s.emit("index:progress", indexing.RebuildProgress{Stage: indexing.ProgressIndexing, Message: err.Error()})
		}
		cancel()
	}
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case change := <-events:
			pending = mergeWorkspaceChanges(pending, change)
			if timer == nil {
				timer = time.NewTimer(300 * time.Millisecond)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(300 * time.Millisecond)
			}
		case <-timerC:
			flush()
			timerC = nil
			timer = nil
		}
	}
}

func (s *Service) processWorkspaceChanges(ctx context.Context, changes platform.WorkspaceChangeSet) error {
	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return err
	}
	changes = dedupeWorkspaceChanges(changes)
	if err := updateIncrementalProjections(ctx, repo, searchIndex, graphStore, s.corpusState(), changes.Changed, changes.Deleted); err != nil {
		return projectionUpdateErr(err)
	}
	for _, id := range changes.Deleted {
		s.emit("note:deleted", map[string]string{"id": string(id)})
	}
	for _, id := range changes.Changed {
		note, err := repo.Read(ctx, id)
		if err == nil {
			s.emit("note:updated", noteDTO(note))
		}
	}
	if len(changes.Changed) > 0 || len(changes.Deleted) > 0 {
		s.emit("graph:updated", map[string]any{"changed": noteIDs(changes.Changed), "deleted": noteIDs(changes.Deleted)})
	}
	s.markDeleted(changes.Deleted...)
	s.markDirty(changes.Changed...)
	return nil
}

func mergeWorkspaceChanges(left, right platform.WorkspaceChangeSet) platform.WorkspaceChangeSet {
	left.Changed = append(left.Changed, right.Changed...)
	left.Deleted = append(left.Deleted, right.Deleted...)
	return dedupeWorkspaceChanges(left)
}

func dedupeWorkspaceChanges(input platform.WorkspaceChangeSet) platform.WorkspaceChangeSet {
	changed := map[domain.NoteID]struct{}{}
	deleted := map[domain.NoteID]struct{}{}
	for _, id := range input.Deleted {
		deleted[id] = struct{}{}
	}
	for _, id := range input.Changed {
		if _, wasDeleted := deleted[id]; !wasDeleted {
			changed[id] = struct{}{}
		}
	}
	out := platform.WorkspaceChangeSet{Changed: make([]domain.NoteID, 0, len(changed)), Deleted: make([]domain.NoteID, 0, len(deleted))}
	for id := range changed {
		out.Changed = append(out.Changed, id)
	}
	for id := range deleted {
		out.Deleted = append(out.Deleted, id)
	}
	sortNoteIDs(out.Changed)
	sortNoteIDs(out.Deleted)
	return out
}

func noteIDs(ids []domain.NoteID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func sortNoteIDs(ids []domain.NoteID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}
func (s *Service) repoSnapshot() (*workspace.FileNoteRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repo == nil {
		return nil, workspaceNotOpenErr()
	}
	return s.repo, nil
}

// listSnapshot returns the repo (required) and graph store (may be nil) for
// serving note lists. Unlike sessionSnapshot it does not require the search index.
func (s *Service) listSnapshot() (*workspace.FileNoteRepository, *graph.SQLiteStore, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repo == nil {
		return nil, nil, false, workspaceNotOpenErr()
	}
	return s.repo, s.graphStore, s.listFromSQLite, nil
}

func (s *Service) sessionSnapshot() (*workspace.FileNoteRepository, *search.BleveIndex, *graph.SQLiteStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repo == nil || s.searchIndex == nil || s.graphStore == nil {
		return nil, nil, nil, workspaceNotOpenErr()
	}
	return s.repo, s.searchIndex, s.graphStore, nil
}

// corpusState returns the live corpus index for the open workspace (nil if none).
func (s *Service) corpusState() *liveCorpus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.corpus
}

func (s *Service) closeStores() error {
	var errs []error
	if s.searchIndex != nil {
		errs = append(errs, s.searchIndex.Close())
	}
	if s.graphStore != nil {
		errs = append(errs, s.graphStore.Close())
	}
	s.searchIndex = nil
	s.graphStore = nil
	s.repo = nil
	s.corpus = nil
	s.listFromSQLite = false
	s.workspace = workspace.Workspace{}
	return errors.Join(errs...)
}

func (s *Service) emit(name string, payload any) {
	if s.events != nil {
		s.events(name, payload)
	}
}

// lockNote acquires the per-note write mutex and returns its unlock function.
func (s *Service) lockNote(id domain.NoteID) func() {
	actual, _ := s.noteLocks.LoadOrStore(id, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func updateIncrementalProjections(ctx context.Context, repo *workspace.FileNoteRepository, searchIndex *search.BleveIndex, graphStore *graph.SQLiteStore, live *liveCorpus, changed []domain.NoteID, deleted []domain.NoteID) error {
	for _, id := range deleted {
		if err := searchIndex.Delete(ctx, id); err != nil {
			return err
		}
		if err := graphStore.DeleteNote(ctx, id); err != nil {
			return err
		}
	}
	if len(changed) == 0 && len(deleted) == 0 {
		return nil
	}
	corpus, err := parseCorpus(ctx, repo)
	if err != nil {
		return err
	}
	// Resync the in-memory corpus from the freshly parsed set so the ID index stays
	// authoritative against external (out-of-app) file changes.
	if live != nil {
		live.Replace(graph.BuildCorpusIndex(corpus))
	}
	parsedByID := make(map[domain.NoteID]domain.ParsedOKFNote, len(corpus))
	ids := make([]domain.NoteID, len(corpus))
	for i, note := range corpus {
		parsedByID[note.ID] = note
		ids[i] = note.ID
	}
	resolver := okf.NewResolver(ids)
	for i := range corpus {
		corpus[i].Links = resolver.ResolveLinks(corpus[i].ID, corpus[i].Links)
	}
	changedSet := map[domain.NoteID]struct{}{}
	for _, id := range changed {
		changedSet[id] = struct{}{}
		_, ok := parsedByID[id]
		if !ok {
			if err := searchIndex.Delete(ctx, id); err != nil {
				return err
			}
			if err := graphStore.DeleteNote(ctx, id); err != nil {
				return err
			}
		}
	}
	for _, parsed := range corpus {
		if err := graphStore.ReplaceOutgoingLinks(ctx, parsed.ID, noteLinks(parsed)); err != nil {
			return err
		}
		if _, ok := changedSet[parsed.ID]; ok {
			if err := graphStore.ReplaceMetadataLinks(ctx, parsed.ID, graph.MetadataMemberships(parsed)); err != nil {
				return err
			}
			if err := graphStore.UpsertNoteMeta(ctx, noteMetaFor(parsed.ID, string(parsed.ID)+".md", parsed.Metadata.Type, parsed.ModifiedAt, parsed.Tags, parsed.Metadata.Favorite)); err != nil {
				return err
			}
			if err := searchIndex.Index(ctx, domain.SearchDocumentFromParsed(parsed, domain.NotePath(string(parsed.ID)+".md"))); err != nil {
				return err
			}
		}
	}
	inference := graph.NewLocalInferenceService(graph.InferenceConfig{})
	for _, parsed := range corpus {
		soft, err := inference.InferLinks(ctx, parsed, corpus)
		if err != nil {
			return err
		}
		if err := graphStore.ReplaceInferredLinks(ctx, parsed.ID, soft); err != nil {
			return err
		}
	}
	return nil
}

// updateOneProjectionFast updates only the edited note's search document and
// hard (outgoing) graph links. Unlike updateOneProjection it does NOT reparse the
// whole corpus or recompute soft-link inference — link *resolution* only needs
// the set of existing note IDs, which the in-memory corpus index serves without a
// filesystem walk. Soft-link inference is scheduled separately off the hot path.
// This keeps save latency independent of corpus size.
func updateOneProjectionFast(ctx context.Context, repo *workspace.FileNoteRepository, searchIndex *search.BleveIndex, graphStore *graph.SQLiteStore, corpus *liveCorpus, note domain.Note) error {
	codec := okf.NewCodec()
	parsed, err := codec.Decode(note.ID, note.Document.Raw, note.ModifiedAt)
	if err != nil {
		return err
	}
	// Upsert first so the resolver ID set reflects this (possibly new) note, then
	// resolve — matching the previous repo.List-after-save ordering.
	ids, err := resolverIDs(ctx, corpus, repo, parsed)
	if err != nil {
		return err
	}
	resolver := okf.NewResolver(ids)
	parsed.Links = resolver.ResolveLinks(parsed.ID, parsed.Links)
	if err := searchIndex.Index(ctx, domain.SearchDocumentFromParsed(parsed, domain.NotePath(string(parsed.ID)+".md"))); err != nil {
		return err
	}
	if err := graphStore.ReplaceOutgoingLinks(ctx, parsed.ID, noteLinks(parsed)); err != nil {
		return err
	}
	if err := graphStore.ReplaceMetadataLinks(ctx, parsed.ID, graph.MetadataMemberships(parsed)); err != nil {
		return err
	}
	return graphStore.UpsertNoteMeta(ctx, noteMetaFor(parsed.ID, string(note.Path), parsed.Metadata.Type, note.ModifiedAt, parsed.Tags, parsed.Metadata.Favorite))
}

// resolverIDs returns the note ID set for link resolution. When the in-memory
// corpus is available it upserts the just-saved note and returns its ID set (no
// filesystem walk); otherwise it falls back to a directory scan.
func resolverIDs(ctx context.Context, corpus *liveCorpus, repo *workspace.FileNoteRepository, parsed domain.ParsedOKFNote) ([]domain.NoteID, error) {
	if corpus != nil {
		corpus.Upsert(parsed)
		return corpus.ResolverIDs(), nil
	}
	summaries, err := repo.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]domain.NoteID, len(summaries))
	for i, summary := range summaries {
		ids[i] = summary.ID
	}
	return ids, nil
}

func updateOneProjection(ctx context.Context, repo *workspace.FileNoteRepository, searchIndex *search.BleveIndex, graphStore *graph.SQLiteStore, live *liveCorpus, note domain.Note) error {
	codec := okf.NewCodec()
	parsed, err := codec.Decode(note.ID, note.Document.Raw, note.ModifiedAt)
	if err != nil {
		return err
	}
	corpus, err := parseCorpus(ctx, repo)
	if err != nil {
		return err
	}
	if live != nil {
		live.Replace(graph.BuildCorpusIndex(corpus))
	}
	ids := make([]domain.NoteID, len(corpus))
	for i, parsedNote := range corpus {
		ids[i] = parsedNote.ID
	}
	resolver := okf.NewResolver(ids)
	parsed.Links = resolver.ResolveLinks(parsed.ID, parsed.Links)
	if err := searchIndex.Index(ctx, domain.SearchDocumentFromParsed(parsed, domain.NotePath(string(parsed.ID)+".md"))); err != nil {
		return err
	}
	if err := graphStore.ReplaceOutgoingLinks(ctx, parsed.ID, noteLinks(parsed)); err != nil {
		return err
	}
	if err := graphStore.ReplaceMetadataLinks(ctx, parsed.ID, graph.MetadataMemberships(parsed)); err != nil {
		return err
	}
	if err := graphStore.UpsertNoteMeta(ctx, noteMetaFor(parsed.ID, string(note.Path), parsed.Metadata.Type, note.ModifiedAt, parsed.Tags, parsed.Metadata.Favorite)); err != nil {
		return err
	}
	inference := graph.NewLocalInferenceService(graph.InferenceConfig{})
	soft, err := inference.InferLinks(ctx, parsed, corpus)
	if err != nil {
		return err
	}
	return graphStore.ReplaceInferredLinks(ctx, parsed.ID, soft)
}

func parseCorpus(ctx context.Context, repo *workspace.FileNoteRepository) ([]domain.ParsedOKFNote, error) {
	summaries, err := repo.List(ctx)
	if err != nil {
		return nil, err
	}
	codec := okf.NewCodec()
	var parsed []domain.ParsedOKFNote
	for _, summary := range summaries {
		note, err := repo.Read(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		parsedNote, err := codec.Decode(note.ID, note.Document.Raw, note.ModifiedAt)
		if err == nil {
			parsed = append(parsed, parsedNote)
		}
	}
	return parsed, nil
}

func noteLinks(note domain.ParsedOKFNote) []domain.NoteLink {
	links := make([]domain.NoteLink, 0, len(note.Links))
	for _, link := range note.Links {
		links = append(links, domain.NoteLink{Source: note.ID, Target: link.RawTarget, ResolvedID: link.ResolvedID, DisplayText: link.DisplayText, Heading: link.Heading, Strength: domain.LinkStrengthHard})
	}
	return links
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

// encodeVersion renders a FileVersion as an opaque token "<modUnixNano>-<size>".
// The zero version encodes to the empty string.
func encodeVersion(v domain.FileVersion) string {
	if v.ModifiedAt.IsZero() && v.Size == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%d", v.ModifiedAt.UnixNano(), v.Size)
}

// decodeVersion parses a token produced by encodeVersion back into a FileVersion.
func decodeVersion(token string) (domain.FileVersion, error) {
	parts := strings.SplitN(strings.TrimSpace(token), "-", 2)
	if len(parts) != 2 {
		return domain.FileVersion{}, fmt.Errorf("malformed version token %q", token)
	}
	nano, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return domain.FileVersion{}, fmt.Errorf("malformed version modtime %q: %w", parts[0], err)
	}
	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return domain.FileVersion{}, fmt.Errorf("malformed version size %q: %w", parts[1], err)
	}
	return domain.FileVersion{ModifiedAt: time.Unix(0, nano), Size: size}, nil
}

func projectionUpdateErr(err error) AppError {
	var decodeErr domain.DecodeError
	if errors.As(err, &decodeErr) {
		return appErr(ErrOKFDecodeFailed, "Could not decode OKF note", err)
	}
	return appErr(ErrProjectionStale, "Could not update derived projections", err)
}
func appErr(code, message string, err error) AppError {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return AppError{Code: code, Message: message, Detail: detail}
}

// workspaceNotOpenErr is the standard error returned by methods that require an
// open workspace. Centralised so the code/message stay consistent.
func workspaceNotOpenErr() AppError {
	return appErr("workspace.not_open", "No workspace is open", nil)
}

func tags(values []string) []domain.Tag {
	out := make([]domain.Tag, len(values))
	for i, value := range values {
		out[i] = domain.Tag(value)
	}
	return out
}

func noteSummaryDTO(note domain.NoteSummary) NoteSummaryDTO {
	tags := make([]string, len(note.Tags))
	for i, tag := range note.Tags {
		tags[i] = string(tag)
	}
	return NoteSummaryDTO{ID: string(note.ID), Title: note.Title, Path: string(note.Path), Type: note.Type, Tags: tags, Favorite: note.Favorite, ModifiedAt: note.ModifiedAt.Format(timeFormat)}
}

func noteRowsToDTOs(rows []graph.NoteRow) []NoteSummaryDTO {
	out := make([]NoteSummaryDTO, len(rows))
	for i, row := range rows {
		out[i] = noteRowToDTO(row)
	}
	return out
}

func noteRowToDTO(row graph.NoteRow) NoteSummaryDTO {
	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}
	return NoteSummaryDTO{ID: row.ID, Title: row.Title, Path: row.Path, Type: row.Type, Tags: tags, Favorite: row.Favorite, ModifiedAt: row.ModifiedAt}
}

// noteMetaFor builds the SQLite note-metadata row for a parsed note. Title uses
// the filename-derived form (matching the sidebar) for stable link resolution.
func noteMetaFor(id domain.NoteID, path string, noteType string, modified time.Time, tags []domain.Tag, favorite bool) graph.NoteMeta {
	return graph.NoteMeta{ID: id, Title: workspace.TitleFromID(id), Path: path, Type: noteType, ModifiedAt: modified, Tags: tags, Favorite: favorite}
}

func noteDTO(note domain.Note) NoteDTO {
	favorite := favoriteFromRawFrontmatter(note.Document.Raw)
	return NoteDTO{ID: string(note.ID), Path: string(note.Path), Content: note.Document.Raw, Favorite: favorite, ModifiedAt: note.ModifiedAt.Format(timeFormat), Version: encodeVersion(note.Version)}
}

func favoriteFromRawFrontmatter(raw string) bool {
	frontmatter, _, ok := rawFrontmatter(raw)
	if !ok {
		return false
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(line, "favorite:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "favorite:"))
		value = strings.Trim(value, `"'`)
		switch strings.ToLower(value) {
		case "true", "yes", "y", "1", "on":
			return true
		default:
			return false
		}
	}
	return false
}

func setFavoriteFrontmatter(raw string, favorite bool) (string, bool) {
	frontmatter, suffix, ok := rawFrontmatter(raw)
	if !ok {
		if !favorite {
			return raw, false
		}
		return "---\nfavorite: true\n---\n" + raw, true
	}
	lines := strings.Split(frontmatter, "\n")
	out := make([]string, 0, len(lines)+1)
	found := false
	changed := false
	for _, line := range lines {
		if strings.HasPrefix(line, "favorite:") {
			found = true
			if favorite {
				if line != "favorite: true" {
					changed = true
				}
				out = append(out, "favorite: true")
			} else {
				changed = true
			}
			continue
		}
		out = append(out, line)
	}
	if favorite && !found {
		out = append(out, "favorite: true")
		changed = true
	}
	if !changed {
		return raw, false
	}
	return "---\n" + strings.Join(out, "\n") + suffix, true
}

func rawFrontmatter(raw string) (string, string, bool) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", false
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return "", "", false
	}
	end += 4
	return normalized[4:end], normalized[end:], true
}

func noteLinkDTO(link domain.NoteLink) NoteLinkDTO {
	resolved := ""
	if link.ResolvedID != nil {
		resolved = string(*link.ResolvedID)
	}
	return NoteLinkDTO{Source: string(link.Source), Target: link.Target, ResolvedID: resolved, DisplayText: link.DisplayText, Heading: link.Heading, Strength: string(link.Strength)}
}

func graphDTO(graph domain.Graph) GraphDTO {
	nodes := make([]GraphNodeDTO, len(graph.Nodes))
	for i, node := range graph.Nodes {
		noteID := ""
		if node.NoteID != nil {
			noteID = string(*node.NoteID)
		}
		nodes[i] = GraphNodeDTO{ID: node.ID, Label: node.Label, Kind: string(node.Kind), NoteID: noteID}
	}
	edges := make([]GraphEdgeDTO, len(graph.Edges))
	for i, edge := range graph.Edges {
		edges[i] = GraphEdgeDTO{ID: edge.ID, Source: edge.Source, Target: edge.Target, Kind: string(edge.Kind), Score: edge.Score}
	}
	return GraphDTO{Nodes: nodes, Edges: edges}
}

func rebuildDTO(result indexing.RebuildResult) RebuildResultDTO {
	return RebuildResultDTO{WorkspaceRoot: result.WorkspaceRoot, TotalNotes: result.TotalNotes, ParsedNotes: result.ParsedNotes, FailedNotes: result.FailedNotes, SearchPath: result.SearchPath, GraphPath: result.GraphPath, StatePath: result.StatePath}
}

func (s *Service) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspace.Root() == "" {
		return "Service(no workspace)"
	}
	return fmt.Sprintf("Service(%s)", s.workspace.Root())
}
