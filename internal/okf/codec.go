package okf

import (
	"bytes"
	"sort"
	"time"

	"GoMental/internal/domain"

	"gopkg.in/yaml.v3"
)

type Codec struct {
	parser Parser
}

func NewCodec() Codec {
	return Codec{parser: NewParser()}
}

func (c Codec) Decode(id domain.NoteID, raw string, modifiedAt time.Time) (domain.ParsedOKFNote, error) {
	return c.parser.ParseNote(id, raw, modifiedAt)
}

func (c Codec) Encode(metadata domain.OKFMetadata, body string) (domain.OKFDocument, error) {
	frontmatter := map[string]any{}
	for key, value := range metadata.Unknown {
		frontmatter[key] = value
	}
	frontmatter["type"] = metadata.Type
	if metadata.Title != "" {
		frontmatter["title"] = metadata.Title
	}
	if metadata.Description != "" {
		frontmatter["description"] = metadata.Description
	}
	if metadata.Resource != "" {
		frontmatter["resource"] = metadata.Resource
	}
	if len(metadata.Tags) > 0 {
		tags := make([]string, len(metadata.Tags))
		for i, tag := range metadata.Tags {
			tags[i] = string(tag)
		}
		sort.Strings(tags)
		frontmatter["tags"] = tags
	}
	if metadata.Timestamp != nil {
		frontmatter["timestamp"] = metadata.Timestamp.UTC().Format(time.RFC3339)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(frontmatter); err != nil {
		return domain.OKFDocument{}, err
	}
	if err := encoder.Close(); err != nil {
		return domain.OKFDocument{}, err
	}
	buf.WriteString("---\n")
	buf.WriteString(body)
	return domain.OKFDocument{Raw: buf.String()}, nil
}
