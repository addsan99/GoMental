package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"GoMental/internal/domain"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	querypkg "github.com/blevesearch/bleve/v2/search/query"
	index "github.com/blevesearch/bleve_index_api"
)

const (
	DefaultLimit        = 20
	SearchSchemaVersion = 3
)

type BleveIndex struct {
	path  string
	index bleve.Index
}

type storedDocument struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	NameText    string   `json:"nameText"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Headings    []string `json:"headings"`
	Tags        []string `json:"tags"`
	Aliases     []string `json:"aliases"`
	LinkTargets []string `json:"linkTargets"`
	Favorite    bool     `json:"favorite"`
	ModifiedAt  int64    `json:"modifiedAt"`
}

func OpenBleveIndex(path string) (*BleveIndex, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	idx, err := bleve.Open(path)
	if errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
		idx, err = bleve.New(path, indexMapping())
	}
	if err != nil {
		return nil, err
	}
	return &BleveIndex{path: path, index: idx}, nil
}

func (b *BleveIndex) Close() error {
	if b == nil || b.index == nil {
		return nil
	}
	return b.index.Close()
}

func (b *BleveIndex) Index(ctx context.Context, document domain.SearchDocument) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.index.Index(string(document.ID), toStored(document))
}

func (b *BleveIndex) Delete(ctx context.Context, id domain.NoteID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.index.Delete(string(id))
}

func (b *BleveIndex) Search(ctx context.Context, query domain.SearchQuery) ([]domain.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = DefaultLimit
	}
	bleveQuery := buildQuery(query)
	request := bleve.NewSearchRequestOptions(bleveQuery, limit, 0, false)
	request.Fields = []string{"id", "path", "title", "favorite"}
	request.Highlight = bleve.NewHighlightWithStyle("html")
	request.Highlight.AddField("title")
	request.Highlight.AddField("body")
	request.Highlight.AddField("headings")
	response, err := b.index.SearchInContext(ctx, request)
	if err != nil {
		return nil, err
	}
	results := make([]domain.SearchResult, 0, len(response.Hits))
	for _, hit := range response.Hits {
		result := domain.SearchResult{ID: domain.NoteID(hit.ID), Score: hit.Score}
		if value, ok := hit.Fields["favorite"].(bool); ok {
			result.Favorite = value
		}
		if value, ok := hit.Fields["path"].(string); ok {
			result.Path = domain.NotePath(value)
		}
		if value, ok := hit.Fields["title"].(string); ok {
			result.Title = value
		}
		for _, fragments := range hit.Fragments {
			result.Fragments = append(result.Fragments, fragments...)
		}
		results = append(results, result)
	}
	return results, nil
}

func (b *BleveIndex) Rebuild(ctx context.Context, documents []domain.SearchDocument) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(b.path); err != nil {
		return err
	}
	idx, err := bleve.New(b.path, indexMapping())
	if err != nil {
		return err
	}
	b.index = idx
	batch := b.index.NewBatch()
	for i, document := range documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := batch.Index(string(document.ID), toStored(document)); err != nil {
			return err
		}
		if (i+1)%100 == 0 {
			if err := b.index.Batch(batch); err != nil {
				return err
			}
			batch = b.index.NewBatch()
		}
	}
	if batch.Size() > 0 {
		return b.index.Batch(batch)
	}
	return nil
}

func indexMapping() mapping.IndexMapping {
	idx := bleve.NewIndexMapping()
	// BM25 handles term-frequency saturation and document-length normalization,
	// which ranks prose notes better than the classic TF-IDF default. Changing
	// this requires a full reindex (Rebuild) since scoring is baked into segments.
	idx.ScoringModel = index.BM25Scoring
	doc := bleve.NewDocumentMapping()
	for _, field := range []string{"nameText", "title", "body", "headings", "aliases", "linkTargets"} {
		m := bleve.NewTextFieldMapping()
		m.Store = true
		doc.AddFieldMappingsAt(field, m)
	}
	keyword := bleve.NewKeywordFieldMapping()
	keyword.Store = true
	doc.AddFieldMappingsAt("id", keyword)
	doc.AddFieldMappingsAt("path", keyword)
	doc.AddFieldMappingsAt("tags", keyword)
	favorite := bleve.NewBooleanFieldMapping()
	favorite.Store = true
	doc.AddFieldMappingsAt("favorite", favorite)
	number := bleve.NewNumericFieldMapping()
	doc.AddFieldMappingsAt("modifiedAt", number)
	idx.DefaultMapping = doc
	return idx
}

func buildQuery(input domain.SearchQuery) querypkg.Query {
	var parts []querypkg.Query
	text := strings.TrimSpace(input.Text)
	if text != "" {
		textQuery := bleve.NewDisjunctionQuery(
			boostedMatch(text, "nameText", 6),
			boostedMatch(text, "title", 5),
			boostedMatch(text, "aliases", 4),
			boostedMatch(text, "headings", 3),
			boostedMatch(text, "tags", 2),
			boostedMatch(text, "body", 1),
			boostedMatch(text, "linkTargets", 1),
		)
		prefix := bleve.NewDisjunctionQuery(
			prefixQuery(text, "nameText", 2.5),
			prefixQuery(text, "title", 2),
			prefixQuery(text, "headings", 1.5),
		)
		parts = append(parts, bleve.NewDisjunctionQuery(textQuery, prefix))
	}
	for _, tag := range input.Tags {
		term := bleve.NewTermQuery(string(tag))
		term.SetField("tags")
		parts = append(parts, term)
	}
	if input.PathPrefix != "" {
		prefix := bleve.NewPrefixQuery(input.PathPrefix)
		prefix.SetField("path")
		parts = append(parts, prefix)
	}
	if input.FavoritesOnly {
		favorite := bleve.NewBoolFieldQuery(true)
		favorite.SetField("favorite")
		parts = append(parts, favorite)
	}
	if len(parts) == 0 {
		return bleve.NewMatchAllQuery()
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return bleve.NewConjunctionQuery(parts...)
}

func boostedMatch(text, field string, boost float64) querypkg.Query {
	q := bleve.NewMatchQuery(text)
	q.SetField(field)
	q.SetBoost(boost)
	return q
}

func prefixQuery(text, field string, boost float64) querypkg.Query {
	q := bleve.NewPrefixQuery(strings.ToLower(text))
	q.SetField(field)
	q.SetBoost(boost)
	return q
}

func toStored(document domain.SearchDocument) storedDocument {
	tags := make([]string, len(document.Tags))
	for i, tag := range document.Tags {
		tags[i] = string(tag)
	}
	return storedDocument{
		ID:          string(document.ID),
		Path:        string(document.Path),
		NameText:    searchableNameText(document),
		Title:       document.Title,
		Body:        document.Body,
		Headings:    document.Headings,
		Tags:        tags,
		Aliases:     document.Aliases,
		LinkTargets: document.LinkTargets,
		Favorite:    document.Favorite,
		ModifiedAt:  document.ModifiedAt,
	}
}

func searchableNameText(document domain.SearchDocument) string {
	id := string(document.ID)
	path := string(document.Path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	normalizedID := strings.NewReplacer("/", " ", "\\", " ", "-", " ", "_", " ").Replace(id)
	normalizedPath := strings.NewReplacer("/", " ", "\\", " ", "-", " ", "_", " ").Replace(path)
	return strings.Join([]string{id, path, base, normalizedID, normalizedPath}, " ")
}

func WorkspaceSearchPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".workspace", "search")
}
