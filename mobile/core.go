// Package mobile exposes the notes-only GoMental core through a gomobile-safe API.
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"GoMental/internal/domain"
	"GoMental/internal/okf"
	"GoMental/internal/search"
	"GoMental/internal/workspace"
)

const maxAssetSize = 25 << 20

// Core serializes repository and index operations while allowing Cancel to be
// called from another Java thread.
type Core struct {
	opMu    sync.Mutex
	stateMu sync.Mutex
	cancel  context.CancelFunc
	closed  bool

	repositoryPath string
	indexPath      string
	workspace      workspace.Workspace
	repository     *workspace.FileNoteRepository
	index          *search.BleveIndex
	notes          []noteDTO
	parsed         map[domain.NoteID]domain.ParsedOKFNote
	remote         string
	ref            string
	commit         string
	gitManaged     bool
	tracked        map[string]struct{}
}

type config struct {
	RepositoryPath string `json:"repositoryPath"`
	DataPath       string `json:"dataPath"`
}

type listQuery struct {
	Text          string   `json:"text"`
	PathPrefix    string   `json:"pathPrefix"`
	Tags          []string `json:"tags"`
	FavoritesOnly bool     `json:"favoritesOnly"`
}

type searchQuery struct {
	Text          string   `json:"text"`
	PathPrefix    string   `json:"pathPrefix"`
	Tags          []string `json:"tags"`
	FavoritesOnly bool     `json:"favoritesOnly"`
	Limit         int      `json:"limit"`
}

type noteDTO struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Path       string    `json:"path"`
	Type       string    `json:"type,omitempty"`
	Tags       []string  `json:"tags"`
	Favorite   bool      `json:"favorite"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type noteDetailDTO struct {
	noteDTO
	Raw           string             `json:"raw"`
	Body          string             `json:"body"`
	Description   string             `json:"description,omitempty"`
	Resource      string             `json:"resource,omitempty"`
	Headings      []headingDTO       `json:"headings"`
	Links         []linkDTO          `json:"links"`
	Metadata      []metadataFieldDTO `json:"metadata"`
	IncomingLinks []incomingLinkDTO  `json:"incomingLinks"`
}

type metadataFieldDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type incomingLinkDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Path        string `json:"path"`
	DisplayText string `json:"displayText,omitempty"`
}

type headingDTO struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Slug  string `json:"slug"`
}

type linkDTO struct {
	RawTarget   string `json:"rawTarget"`
	ResolvedID  string `json:"resolvedId,omitempty"`
	DisplayText string `json:"displayText,omitempty"`
	Heading     string `json:"heading,omitempty"`
	Kind        string `json:"kind"`
}

type statusDTO struct {
	Ready          bool   `json:"ready"`
	RepositoryPath string `json:"repositoryPath"`
	NoteCount      int    `json:"noteCount"`
	Remote         string `json:"remote,omitempty"`
	Ref            string `json:"ref,omitempty"`
	Commit         string `json:"commit,omitempty"`
}

type searchResultDTO struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Score     float64  `json:"score"`
	Fragments []string `json:"fragments"`
	Favorite  bool     `json:"favorite"`
}

// Open creates a mobile core. An absent repository is allowed so a subsequent
// Sync call can perform the initial clone.
func Open(configJSON string) (*Core, error) {
	var cfg config
	if err := decodeJSON(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("mobile: invalid config: %w", err)
	}
	if strings.TrimSpace(cfg.RepositoryPath) == "" {
		return nil, errors.New("mobile: repositoryPath is required")
	}
	if strings.TrimSpace(cfg.DataPath) == "" {
		return nil, errors.New("mobile: dataPath is required")
	}
	repositoryPath, err := filepath.Abs(cfg.RepositoryPath)
	if err != nil {
		return nil, fmt.Errorf("mobile: repository path: %w", err)
	}
	dataPath, err := filepath.Abs(cfg.DataPath)
	if err != nil {
		return nil, fmt.Errorf("mobile: data path: %w", err)
	}
	if pathsOverlap(repositoryPath, dataPath) {
		return nil, errors.New("mobile: dataPath must be outside repositoryPath")
	}
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return nil, fmt.Errorf("mobile: create data path: %w", err)
	}

	core := &Core{
		repositoryPath: repositoryPath,
		indexPath:      filepath.Join(dataPath, "search"),
		parsed:         make(map[domain.NoteID]domain.ParsedOKFNote),
	}
	if info, statErr := os.Stat(repositoryPath); statErr == nil && info.IsDir() {
		if err := core.prepareGitWorkspace(); err != nil {
			return nil, err
		}
		ctx, done, beginErr := core.begin()
		if beginErr != nil {
			return nil, beginErr
		}
		core.loadGitStatus()
		err = core.reload(ctx)
		done()
		if err != nil {
			_ = core.Close()
			return nil, err
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("mobile: inspect repository: %w", statErr)
	}
	return core, nil
}

// Status returns JSON describing whether a local repository is ready.
func (c *Core) Status() (string, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.ensureOpen(); err != nil {
		return "", err
	}
	return encodeJSON(statusDTO{
		Ready:          c.repository != nil,
		RepositoryPath: c.repositoryPath,
		NoteCount:      len(c.notes),
		Remote:         c.remote,
		Ref:            c.ref,
		Commit:         c.commit,
	})
}

// Rebuild rescans the current checkout and atomically replaces its search index.
func (c *Core) Rebuild() error {
	ctx, done, err := c.begin()
	if err != nil {
		return err
	}
	defer done()
	return c.reload(ctx)
}

// ListNotes returns a filtered JSON note collection.
func (c *Core) ListNotes(queryJSON string) (string, error) {
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	if c.repository == nil {
		return "", errors.New("mobile: repository is not ready")
	}
	var query listQuery
	if err := decodeOptionalJSON(queryJSON, &query); err != nil {
		return "", fmt.Errorf("mobile: invalid list query: %w", err)
	}
	text := strings.ToLower(strings.TrimSpace(query.Text))
	prefix := strings.ToLower(filepath.ToSlash(strings.TrimSpace(query.PathPrefix)))
	tags := stringSet(query.Tags)
	items := make([]noteDTO, 0, len(c.notes))
	for _, note := range c.notes {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if text != "" && !strings.Contains(strings.ToLower(note.Title+" "+note.ID+" "+note.Path), text) {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(note.Path), prefix) {
			continue
		}
		if query.FavoritesOnly && !note.Favorite {
			continue
		}
		if !containsAll(note.Tags, tags) {
			continue
		}
		items = append(items, note)
	}
	return encodeJSON(struct {
		Notes []noteDTO `json:"notes"`
	}{Notes: items})
}

// ReadNote returns parsed OKF content and resolved links as JSON.
func (c *Core) ReadNote(id string) (string, error) {
	_, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	if c.repository == nil {
		return "", errors.New("mobile: repository is not ready")
	}
	normalized, err := c.workspace.NormalizeNoteID(id)
	if err != nil {
		return "", err
	}
	parsed, ok := c.parsed[normalized]
	if !ok {
		return "", fmt.Errorf("mobile: note %q is not a valid OKF document", normalized)
	}
	summary := c.summary(normalized)
	detail := noteDetailDTO{
		noteDTO:       summary,
		Raw:           parsed.Document.Raw,
		Body:          parsed.Body,
		Description:   parsed.Metadata.Description,
		Resource:      parsed.Metadata.Resource,
		Headings:      make([]headingDTO, 0, len(parsed.Headings)),
		Links:         make([]linkDTO, 0, len(parsed.Links)),
		Metadata:      metadataFields(parsed),
		IncomingLinks: c.incomingLinks(normalized),
	}
	for _, heading := range parsed.Headings {
		detail.Headings = append(detail.Headings, headingDTO{Level: heading.Level, Text: heading.Text, Slug: heading.Slug})
	}
	for _, link := range parsed.Links {
		item := linkDTO{RawTarget: link.RawTarget, DisplayText: link.DisplayText, Heading: link.Heading, Kind: string(link.Kind)}
		if link.ResolvedID != nil {
			item.ResolvedID = string(*link.ResolvedID)
		}
		detail.Links = append(detail.Links, item)
	}
	return encodeJSON(detail)
}

func metadataFields(note domain.ParsedOKFNote) []metadataFieldDTO {
	fields := make([]metadataFieldDTO, 0, len(note.Metadata.Unknown)+7)
	add := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			fields = append(fields, metadataFieldDTO{Key: key, Value: value})
		}
	}
	add("type", note.Metadata.Type)
	add("title", note.Metadata.Title)
	add("description", note.Metadata.Description)
	add("resource", note.Metadata.Resource)
	if len(note.Metadata.Tags) > 0 {
		add("tags", strings.Join(tagsToStrings(note.Metadata.Tags), ", "))
	}
	if note.Metadata.Favorite {
		add("favorite", "true")
	}
	if note.Metadata.Timestamp != nil {
		add("timestamp", note.Metadata.Timestamp.Format(time.RFC3339))
	}
	keys := make([]string, 0, len(note.Metadata.Unknown))
	for key := range note.Metadata.Unknown {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(key, metadataValue(note.Metadata.Unknown[key]))
	}
	return fields
}

func metadataValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		encoded, err := json.Marshal(typed)
		if err == nil {
			return string(encoded)
		}
		return fmt.Sprint(typed)
	}
}

func (c *Core) incomingLinks(target domain.NoteID) []incomingLinkDTO {
	seen := make(map[domain.NoteID]struct{})
	items := make([]incomingLinkDTO, 0)
	for sourceID, note := range c.parsed {
		for _, link := range note.Links {
			if link.ResolvedID == nil || *link.ResolvedID != target {
				continue
			}
			if _, exists := seen[sourceID]; exists {
				continue
			}
			seen[sourceID] = struct{}{}
			summary := c.summary(sourceID)
			items = append(items, incomingLinkDTO{ID: summary.ID, Title: summary.Title, Path: summary.Path, DisplayText: link.DisplayText})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
	return items
}

// Search queries the persistent Bleve index and returns JSON results.
func (c *Core) Search(queryJSON string) (string, error) {
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	if c.index == nil {
		return "", errors.New("mobile: search index is not ready")
	}
	var query searchQuery
	if err := decodeJSON(queryJSON, &query); err != nil {
		return "", fmt.Errorf("mobile: invalid search query: %w", err)
	}
	tags := make([]domain.Tag, len(query.Tags))
	for i, tag := range query.Tags {
		tags[i] = domain.Tag(strings.ToLower(strings.TrimSpace(tag)))
	}
	results, err := c.index.Search(ctx, domain.SearchQuery{
		Text: query.Text, Tags: tags, PathPrefix: query.PathPrefix,
		FavoritesOnly: query.FavoritesOnly, Limit: query.Limit,
	})
	if err != nil {
		return "", err
	}
	items := make([]searchResultDTO, len(results))
	for i, result := range results {
		items[i] = searchResultDTO{
			ID: string(result.ID), Path: string(result.Path), Title: result.Title,
			Score: result.Score, Fragments: result.Fragments, Favorite: result.Favorite,
		}
	}
	return encodeJSON(struct {
		Results []searchResultDTO `json:"results"`
	}{Results: items})
}

// LoadAsset reads a note-relative asset while preventing workspace traversal.
func (c *Core) LoadAsset(noteID, rawPath string) ([]byte, error) {
	_, done, err := c.begin()
	if err != nil {
		return nil, err
	}
	defer done()
	if c.repository == nil {
		return nil, errors.New("mobile: repository is not ready")
	}
	id, err := c.workspace.NormalizeNoteID(noteID)
	if err != nil {
		return nil, err
	}
	assetPath, err := resolveAssetPath(c.workspace.Root(), id, rawPath)
	if err != nil {
		return nil, err
	}
	if c.gitManaged {
		relative, relErr := filepath.Rel(c.workspace.Root(), assetPath)
		if relErr != nil {
			return nil, relErr
		}
		if _, ok := c.tracked[filepath.ToSlash(relative)]; !ok {
			return nil, errors.New("mobile: asset is not tracked by git")
		}
	}
	if err := ensureResolvedInside(c.workspace.Root(), assetPath); err != nil {
		return nil, err
	}
	info, err := os.Stat(assetPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("mobile: asset is not a regular file")
	}
	if info.Size() > maxAssetSize {
		return nil, fmt.Errorf("mobile: asset exceeds %d bytes", maxAssetSize)
	}
	return os.ReadFile(assetPath)
}

// Cancel interrupts the current long-running operation.
func (c *Core) Cancel() {
	c.stateMu.Lock()
	cancel := c.cancel
	c.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close releases the search index. It is safe to call more than once.
func (c *Core) Close() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return nil
	}
	c.closed = true
	cancel := c.cancel
	c.cancel = nil
	c.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c.index != nil {
		err := c.index.Close()
		c.index = nil
		return err
	}
	return nil
}

func (c *Core) begin() (context.Context, func(), error) {
	c.opMu.Lock()
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		c.opMu.Unlock()
		return nil, nil, errors.New("mobile: core is closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.stateMu.Unlock()
	done := func() {
		cancel()
		c.stateMu.Lock()
		c.cancel = nil
		c.stateMu.Unlock()
		c.opMu.Unlock()
	}
	return ctx, done, nil
}

func (c *Core) ensureOpen() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.closed {
		return errors.New("mobile: core is closed")
	}
	return nil
}

func (c *Core) reload(ctx context.Context) error {
	ws, err := workspace.Open(c.repositoryPath)
	if err != nil {
		return fmt.Errorf("mobile: open repository workspace: %w", err)
	}
	repository := workspace.NewFileNoteRepository(ws)
	summaries, err := repository.List(ctx)
	if err != nil {
		return fmt.Errorf("mobile: scan notes: %w", err)
	}
	safeSummaries := make([]domain.NoteSummary, 0, len(summaries))
	for _, summary := range summaries {
		if c.gitManaged {
			if _, ok := c.tracked[string(summary.Path)]; !ok {
				continue
			}
		}
		path, pathErr := ws.PathForNoteID(summary.ID)
		if pathErr != nil {
			return pathErr
		}
		info, pathErr := os.Lstat(path)
		if pathErr != nil {
			return pathErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		safeSummaries = append(safeSummaries, summary)
	}
	summaries = safeSummaries
	parser := okf.NewParser()
	parsed := make(map[domain.NoteID]domain.ParsedOKFNote, len(summaries))
	ids := make([]domain.NoteID, 0, len(summaries))
	for _, summary := range summaries {
		note, readErr := repository.Read(ctx, summary.ID)
		if readErr != nil {
			return fmt.Errorf("mobile: read %s: %w", summary.ID, readErr)
		}
		item, parseErr := parser.ParseNote(note.ID, note.Document.Raw, note.ModifiedAt)
		if parseErr == nil {
			parsed[note.ID] = item
			ids = append(ids, note.ID)
		}
	}
	resolver := okf.NewResolver(ids)
	for id, note := range parsed {
		note.Links = resolver.ResolveLinks(note.ID, note.Links)
		parsed[id] = note
	}

	notes := make([]noteDTO, 0, len(summaries))
	documents := make([]domain.SearchDocument, 0, len(summaries))
	for _, summary := range summaries {
		if err := ctx.Err(); err != nil {
			return err
		}
		note, readErr := repository.Read(ctx, summary.ID)
		if readErr != nil {
			return fmt.Errorf("mobile: read %s: %w", summary.ID, readErr)
		}
		item := noteDTO{ID: string(summary.ID), Title: summary.Title, Path: string(summary.Path), Tags: []string{}, ModifiedAt: summary.ModifiedAt}
		if parsedNote, ok := parsed[summary.ID]; ok {
			item.Title = parsedNote.Title
			item.Type = parsedNote.Metadata.Type
			item.Tags = tagsToStrings(parsedNote.Tags)
			item.Favorite = parsedNote.Metadata.Favorite
			documents = append(documents, domain.SearchDocumentFromParsed(parsedNote, summary.Path))
		} else {
			documents = append(documents, okf.SearchDocumentFromRaw(note.ID, note.Path, note.Document.Raw, note.ModifiedAt))
		}
		notes = append(notes, item)
	}
	if err := c.replaceIndex(ctx, documents); err != nil {
		return fmt.Errorf("mobile: rebuild search: %w", err)
	}
	c.workspace = ws
	c.repository = repository
	c.notes = notes
	c.parsed = parsed
	return nil
}

func (c *Core) replaceIndex(ctx context.Context, documents []domain.SearchDocument) error {
	nextPath := c.indexPath + ".next"
	backupPath := c.indexPath + ".previous"
	if err := os.RemoveAll(nextPath); err != nil {
		return err
	}
	next, err := search.OpenBleveIndex(nextPath)
	if err != nil {
		return err
	}
	if err := next.Rebuild(ctx, documents); err != nil {
		_ = next.Close()
		_ = os.RemoveAll(nextPath)
		return err
	}
	if err := next.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(backupPath); err != nil {
		return err
	}
	hadPrevious := c.index != nil
	if hadPrevious {
		if err := c.index.Close(); err != nil {
			return err
		}
		c.index = nil
	}
	if err := os.Rename(c.indexPath, backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		if reopenErr := c.reopenIndex(); reopenErr != nil {
			return fmt.Errorf("move current index: %w; reopen current index: %v", err, reopenErr)
		}
		return err
	}
	if err := os.Rename(nextPath, c.indexPath); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backupPath, c.indexPath); restoreErr != nil {
				return fmt.Errorf("activate index: %w; restore previous index: %v", err, restoreErr)
			}
			if reopenErr := c.reopenIndex(); reopenErr != nil {
				return fmt.Errorf("activate index: %w; reopen previous index: %v", err, reopenErr)
			}
		}
		return err
	}
	index, err := search.OpenBleveIndex(c.indexPath)
	if err != nil {
		removeErr := os.RemoveAll(c.indexPath)
		if hadPrevious {
			if restoreErr := os.Rename(backupPath, c.indexPath); restoreErr != nil {
				return fmt.Errorf("open new index: %w; remove new index: %v; restore previous index: %v", err, removeErr, restoreErr)
			}
			if reopenErr := c.reopenIndex(); reopenErr != nil {
				return fmt.Errorf("open new index: %w; remove new index: %v; reopen previous index: %v", err, removeErr, reopenErr)
			}
		} else if removeErr != nil {
			return fmt.Errorf("open new index: %w; remove new index: %v", err, removeErr)
		}
		return err
	}
	c.index = index
	_ = os.RemoveAll(backupPath)
	return nil
}

func (c *Core) reopenIndex() error {
	index, err := search.OpenBleveIndex(c.indexPath)
	if err != nil {
		c.index = nil
		return err
	}
	c.index = index
	return nil
}

func (c *Core) summary(id domain.NoteID) noteDTO {
	for _, note := range c.notes {
		if note.ID == string(id) {
			return note
		}
	}
	return noteDTO{ID: string(id), Tags: []string{}}
}

func resolveAssetPath(root string, noteID domain.NoteID, rawPath string) (string, error) {
	value := strings.TrimSpace(rawPath)
	if parsed, err := url.PathUnescape(value); err == nil {
		value = parsed
	}
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		value = value[:index]
	}
	value = strings.ReplaceAll(value, `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "://") {
		return "", errors.New("mobile: invalid asset path")
	}
	notePath := filepath.FromSlash(string(noteID) + ".md")
	target := filepath.Clean(filepath.Join(root, filepath.Dir(notePath), filepath.FromSlash(value)))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("mobile: asset path escapes repository")
	}
	return target, nil
}

func ensureResolvedInside(root, target string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("mobile: resolve repository path: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("mobile: resolved asset path escapes repository")
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	inside := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
	}
	return inside(a, b) || inside(b, a)
}

func decodeJSON(value string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func decodeOptionalJSON(value string, target any) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return decodeJSON(value, target)
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}

func tagsToStrings(tags []domain.Tag) []string {
	values := make([]string, len(tags))
	for i, tag := range tags {
		values[i] = string(tag)
	}
	sort.Strings(values)
	return values
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if normalized := strings.ToLower(strings.TrimSpace(value)); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func containsAll(values []string, required map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	available := stringSet(values)
	for value := range required {
		if _, ok := available[value]; !ok {
			return false
		}
	}
	return true
}
