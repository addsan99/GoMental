package importers

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"strings"

	"GoMental/internal/domain"
)

var ErrNoImporter = errors.New("no importer can handle source")

type ExtractedPage struct {
	Title        string
	CanonicalURL string
	JSONLD       []json.RawMessage
	MainText     string
	Headings     []string
	ListItems    []string
	ImageURLs    []string
}

type Importer interface {
	ID() string
	CanImport(ctx context.Context, source ImportSource) (ImportMatch, error)
	Import(ctx context.Context, source ImportSource) (*ImportResult, error)
}

type ImportSource struct {
	URL         string
	ContentType string
	HTML        []byte
	Page        *ExtractedPage
}

type ImportMatch struct {
	ImporterID string
	NoteType   string
	Confidence float64
}

type ImportResult struct {
	Document   domain.Note
	Links      []domain.NoteLink
	Warnings   []string
	Confidence float64
}

type Registry struct {
	importers []Importer
}

func NewRegistry(importers ...Importer) Registry {
	return Registry{importers: append([]Importer(nil), importers...)}
}

func DefaultRegistry() Registry {
	return NewRegistry(NewRecipeImporter())
}

func (r Registry) Match(ctx context.Context, source ImportSource) (Importer, ImportMatch, error) {
	bestIndex := -1
	var best ImportMatch
	for i, importer := range r.importers {
		if err := ctx.Err(); err != nil {
			return nil, ImportMatch{}, err
		}
		match, err := importer.CanImport(ctx, source)
		if err != nil {
			return nil, ImportMatch{}, err
		}
		if match.Confidence <= 0 {
			continue
		}
		if bestIndex < 0 || match.Confidence > best.Confidence {
			bestIndex = i
			best = match
		}
	}
	if bestIndex < 0 {
		return nil, ImportMatch{}, ErrNoImporter
	}
	return r.importers[bestIndex], best, nil
}

func (r Registry) Import(ctx context.Context, source ImportSource) (*ImportResult, error) {
	importer, _, err := r.Match(ctx, source)
	if err != nil {
		return nil, err
	}
	return importer.Import(ctx, source)
}

func (s ImportSource) ExtractedPage() (ExtractedPage, error) {
	if s.Page != nil {
		return *s.Page, nil
	}
	return ExtractPage(s)
}

var (
	jsonLDScriptPattern = regexp.MustCompile(`(?is)<script\b[^>]*type\s*=\s*["']application/ld\+json["'][^>]*>(.*?)</script>`)
	titlePattern        = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	canonicalPattern    = regexp.MustCompile(`(?is)<link\b[^>]*rel\s*=\s*["'][^"']*\bcanonical\b[^"']*["'][^>]*>`)
	hrefPattern         = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']+)["']`)
	tagPattern          = regexp.MustCompile(`(?is)<[^>]+>`)
)

func ExtractPage(source ImportSource) (ExtractedPage, error) {
	page := ExtractedPage{CanonicalURL: strings.TrimSpace(source.URL)}
	rawHTML := string(source.HTML)
	if match := titlePattern.FindStringSubmatch(rawHTML); len(match) == 2 {
		page.Title = cleanHTMLText(match[1])
	}
	if link := canonicalPattern.FindString(rawHTML); link != "" {
		if match := hrefPattern.FindStringSubmatch(link); len(match) == 2 {
			page.CanonicalURL = html.UnescapeString(strings.TrimSpace(match[1]))
		}
	}
	for _, match := range jsonLDScriptPattern.FindAllStringSubmatch(rawHTML, -1) {
		if len(match) != 2 {
			continue
		}
		body := strings.TrimSpace(html.UnescapeString(match[1]))
		if body == "" {
			continue
		}
		if !json.Valid([]byte(body)) {
			return page, errors.New("invalid JSON-LD script")
		}
		page.JSONLD = append(page.JSONLD, json.RawMessage(body))
	}
	return page, nil
}

func cleanHTMLText(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(html.UnescapeString(value)), " ")
}
