package okf

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"GoMental/internal/domain"

	"gopkg.in/yaml.v3"
)

const Version = "0.1"

var (
	headingPattern      = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*$`)
	markdownLinkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	wikiLinkPattern     = regexp.MustCompile(`\[\[([^\]|#]+)(?:#([^\]|]+))?(?:\|([^\]]+))?\]\]`)
	inlineMarkupPattern = regexp.MustCompile("[`*_>#]+")
)

type Parser struct{}

func NewParser() Parser {
	return Parser{}
}

func (p Parser) ParseNote(id domain.NoteID, raw string, modifiedAt time.Time) (domain.ParsedOKFNote, error) {
	frontmatter, body, err := splitFrontmatter(raw)
	if err != nil {
		return domain.ParsedOKFNote{}, err
	}
	metadata, err := parseMetadata(frontmatter)
	if err != nil {
		return domain.ParsedOKFNote{}, err
	}
	if strings.TrimSpace(metadata.Type) == "" {
		return domain.ParsedOKFNote{}, domain.DecodeError{Code: "okf.missing_type", Message: "OKF concept document is missing required type"}
	}
	headings := extractHeadings(body)
	links := extractLinks(id, body)
	title := chooseTitle(metadata.Title, headings, id)
	return domain.ParsedOKFNote{
		ID:         id,
		Document:   domain.OKFDocument{Raw: raw},
		Metadata:   metadata,
		Title:      title,
		Body:       body,
		PlainText:  plainText(body),
		Headings:   headings,
		Tags:       metadata.Tags,
		Links:      links,
		ModifiedAt: modifiedAt,
	}, nil
}

func splitFrontmatter(raw string) (string, string, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", domain.DecodeError{Code: "okf.missing_frontmatter", Message: "OKF concept document must start with YAML frontmatter"}
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return "", "", domain.DecodeError{Code: "okf.unclosed_frontmatter", Message: "OKF concept document frontmatter is not closed"}
	}
	end += 4
	frontmatter := normalized[4:end]
	bodyStart := end + len("\n---")
	if len(normalized) > bodyStart && normalized[bodyStart] == '\n' {
		bodyStart++
	}
	return frontmatter, normalized[bodyStart:], nil
}

func parseMetadata(frontmatter string) (domain.OKFMetadata, error) {
	var values map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &values); err != nil {
		return domain.OKFMetadata{}, domain.DecodeError{Code: "okf.invalid_frontmatter", Message: "OKF frontmatter is not valid YAML", Detail: err.Error()}
	}
	metadata := domain.OKFMetadata{Unknown: map[string]any{}}
	for key, value := range values {
		switch key {
		case "type":
			metadata.Type = scalarString(value)
		case "title":
			metadata.Title = scalarString(value)
		case "description":
			metadata.Description = scalarString(value)
		case "resource":
			metadata.Resource = scalarString(value)
		case "tags":
			metadata.Tags = parseTags(value)
		case "timestamp":
			metadata.Timestamp = parseTimestamp(value)
		default:
			metadata.Unknown[key] = value
		}
	}
	sort.Slice(metadata.Tags, func(i, j int) bool { return metadata.Tags[i] < metadata.Tags[j] })
	return metadata, nil
}

func scalarString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func parseTags(value any) []domain.Tag {
	seen := map[string]struct{}{}
	var tags []domain.Tag
	add := func(raw string) {
		tag := normalizeTag(raw)
		if tag == "" {
			return
		}
		if _, ok := seen[tag]; ok {
			return
		}
		seen[tag] = struct{}{}
		tags = append(tags, domain.Tag(tag))
	}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			add(fmt.Sprint(item))
		}
	case []string:
		for _, item := range typed {
			add(item)
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			add(item)
		}
	default:
		add(fmt.Sprint(typed))
	}
	return tags
}

func normalizeTag(raw string) string {
	tag := strings.TrimSpace(raw)
	tag = strings.TrimPrefix(tag, "#")
	tag = strings.ToLower(tag)
	tag = strings.ReplaceAll(tag, " ", "-")
	return tag
}

func parseTimestamp(value any) *time.Time {
	switch typed := value.(type) {
	case time.Time:
		return &typed
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if parsed, err := time.Parse(layout, typed); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func extractHeadings(body string) []domain.Heading {
	matches := headingPattern.FindAllStringSubmatch(body, -1)
	headings := make([]domain.Heading, 0, len(matches))
	for _, match := range matches {
		text := strings.TrimSpace(strings.TrimRight(match[2], "# "))
		headings = append(headings, domain.Heading{Level: len(match[1]), Text: text, Slug: slug(text)})
	}
	return headings
}

func extractLinks(source domain.NoteID, body string) []domain.ParsedLink {
	var links []domain.ParsedLink
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(body, -1) {
		if strings.HasPrefix(match[0], "!") {
			continue
		}
		raw, heading := splitTargetHeading(match[2])
		links = append(links, domain.ParsedLink{
			Source:      source,
			RawTarget:   raw,
			DisplayText: strings.TrimSpace(match[1]),
			Heading:     heading,
			Kind:        domain.LinkKindMarkdown,
			Strength:    domain.LinkStrengthHard,
		})
	}
	for _, match := range wikiLinkPattern.FindAllStringSubmatch(body, -1) {
		links = append(links, domain.ParsedLink{
			Source:      source,
			RawTarget:   strings.TrimSpace(match[1]),
			DisplayText: strings.TrimSpace(match[3]),
			Heading:     strings.TrimSpace(match[2]),
			Kind:        domain.LinkKindWiki,
			Strength:    domain.LinkStrengthHard,
		})
	}
	return links
}

func splitTargetHeading(raw string) (string, string) {
	parts := strings.SplitN(raw, "#", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func chooseTitle(frontmatterTitle string, headings []domain.Heading, id domain.NoteID) string {
	if strings.TrimSpace(frontmatterTitle) != "" {
		return strings.TrimSpace(frontmatterTitle)
	}
	for _, heading := range headings {
		if heading.Level == 1 && heading.Text != "" {
			return heading.Text
		}
	}
	if len(headings) > 0 && headings[0].Text != "" {
		return headings[0].Text
	}
	parts := strings.Split(string(id), "/")
	return parts[len(parts)-1]
}

func plainText(body string) string {
	text := markdownLinkPattern.ReplaceAllString(body, "$1")
	text = wikiLinkPattern.ReplaceAllString(text, "$1")
	text = headingPattern.ReplaceAllString(text, "$2")
	text = inlineMarkupPattern.ReplaceAllString(text, "")
	fields := strings.Fields(text)
	return strings.Join(fields, " ")
}

func slug(text string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
