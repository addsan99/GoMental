package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"GoMental/internal/application"
)

// Tool is one MCP tool: a name, an LLM-facing description, a JSON-Schema input,
// and a handler that returns the tool result text (usually compact JSON).
type Tool struct {
	Name        string                                                          `json:"name"`
	Description string                                                          `json:"description"`
	InputSchema map[string]any                                                  `json:"inputSchema"`
	Handler     func(ctx context.Context, args json.RawMessage) (string, error) `json:"-"`
}

func obj(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func (s *Server) buildTools() []Tool {
	return []Tool{
		{
			Name:        "search_wiki",
			Description: "Search the wiki by full text (and optional tags/path prefix). This is the primary retrieval tool: it returns ranked notes with highlighted fragments. Use it first to find relevant notes before reading them.",
			InputSchema: obj(map[string]any{
				"query":      strProp("Free-text query."),
				"tags":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional tag filters (AND)."},
				"pathPrefix": strProp("Optional note-path prefix filter."),
				"limit":      intProp("Max results (default 10)."),
			}, "query"),
			Handler: s.searchWiki,
		},
		{
			Name:        "read_note",
			Description: "Read a note's full OKF content by id. Returns the content plus a version token; pass that token to edit_note to avoid clobbering concurrent edits.",
			InputSchema: obj(map[string]any{"id": strProp("Note id (may contain '/').")}, "id"),
			Handler:     s.readNote,
		},
		{
			Name:        "list_notes",
			Description: "List notes, optionally filtered by path prefix, tag, and/or type (the note's `type` frontmatter, matched exactly and case-insensitively). Returns id, title, path, type, tags.",
			InputSchema: obj(map[string]any{
				"pathPrefix": strProp("Optional note-path/id prefix filter."),
				"tag":        strProp("Optional single tag filter."),
				"type":       strProp("Optional note-type filter (exact, case-insensitive match on the note's `type` frontmatter)."),
			}),
			Handler: s.listNotes,
		},
		{
			Name:        "create_note",
			Description: "Create a new note. mode='create' (default) fails if the id exists; 'upsert' writes regardless; 'unique' auto-suffixes the id to a free one. Returns the final id and version.",
			InputSchema: obj(map[string]any{
				"id":      strProp("Desired note id (may contain '/')."),
				"content": strProp("Full OKF/Markdown content."),
				"mode":    map[string]any{"type": "string", "enum": []string{"create", "upsert", "unique"}, "description": "Collision behavior."},
			}, "id", "content"),
			Handler: s.createNote,
		},
		{
			Name:        "edit_note",
			Description: "Replace a note's content using optimistic concurrency. Pass base_version from a prior read_note; if the note changed since, the edit is rejected as a conflict (re-read and retry). Omit base_version to force-overwrite.",
			InputSchema: obj(map[string]any{
				"id":           strProp("Note id."),
				"content":      strProp("New full content."),
				"base_version": strProp("Version token from read_note (optional; omit to force)."),
			}, "id", "content"),
			Handler: s.editNote,
		},
		{
			Name:        "upload_asset",
			Description: "Upload an image asset (screenshot, exported diagram, photo) and attach it to a note. Provide the raw image bytes base64-encoded. Returns the workspace-relative path and a ready-to-paste Markdown image tag — insert that tag into the note body with edit_note. Supported: PNG, JPEG, GIF, WebP, SVG; max 25 MB. For flowcharts/sequence/ER diagrams prefer a text `mermaid` code block instead (no upload needed, and it stays editable).",
			InputSchema: obj(map[string]any{
				"id":          strProp("Note id the asset belongs to (may contain '/'). The image is stored alongside this note."),
				"file_name":   strProp("Original file name, used to derive the stored name and extension (e.g. 'login-flow.png')."),
				"mime_type":   strProp("Image MIME type: image/png, image/jpeg, image/gif, image/webp, or image/svg+xml. Optional — sniffed from the data if omitted."),
				"data_base64": strProp("The image bytes, base64-encoded (standard encoding, no data: prefix)."),
			}, "id", "file_name", "data_base64"),
			Handler: s.uploadAsset,
		},
		{
			Name:        "backlinks",
			Description: "List notes that link to the given note id (incoming links).",
			InputSchema: obj(map[string]any{"id": strProp("Note id.")}, "id"),
			Handler:     s.backlinks,
		},
		{
			Name:        "neighborhood",
			Description: "Return the local knowledge-graph neighborhood around a note id, up to the given depth (default 1). Useful for graph-aware retrieval.",
			InputSchema: obj(map[string]any{
				"id":    strProp("Note id."),
				"depth": intProp("Traversal depth (default 1)."),
			}, "id"),
			Handler: s.neighborhood,
		},
		{
			Name:        "expand_context",
			Description: "Read a note AND the content of its graph neighborhood in one call: returns the focus note's full content plus, for each neighboring note up to the given depth (default 1), a title, an excerpt, and how it connects. Use this instead of chaining read_note + neighborhood + multiple read_notes when answering a question that needs connected context.",
			InputSchema: obj(map[string]any{
				"id":    strProp("Note id (may contain '/')."),
				"depth": intProp("Neighborhood depth (default 1)."),
			}, "id"),
			Handler: s.expandContext,
		},
		{
			Name:        "explain_link",
			Description: "Explain why two notes relate: returns whether they are related, whether a hard link exists, a combined score, and the evidence (title mentions, shared tags/type/headings) plus a one-line summary. Use it to justify a connection before citing it, or to sanity-check a link before authoring one.",
			InputSchema: obj(map[string]any{
				"source": strProp("Source note id."),
				"target": strProp("Target note id."),
			}, "source", "target"),
			Handler: s.explainLink,
		},
	}
}

func (s *Server) expandContext(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID    string `json:"id"`
		Depth int    `json:"depth"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Depth <= 0 {
		args.Depth = 1
	}
	dto, err := s.service.ExpandContext(ctx, args.ID, args.Depth)
	if err != nil {
		return "", err
	}
	return jsonString(dto), nil
}

func (s *Server) explainLink(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	dto, err := s.service.ExplainLink(ctx, args.Source, args.Target)
	if err != nil {
		return "", err
	}
	return jsonString(dto), nil
}

func (s *Server) searchWiki(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Query      string   `json:"query"`
		Tags       []string `json:"tags"`
		PathPrefix string   `json:"pathPrefix"`
		Limit      int      `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}
	results, err := s.service.Search(ctx, application.SearchQueryDTO{Text: args.Query, Tags: args.Tags, PathPrefix: args.PathPrefix, Limit: args.Limit})
	if err != nil {
		return "", err
	}
	return jsonString(map[string]any{"results": results, "count": len(results)}), nil
}

func (s *Server) readNote(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	note, err := s.service.ReadNote(ctx, args.ID)
	if err != nil {
		return "", err
	}
	return jsonString(map[string]any{"id": note.ID, "path": note.Path, "content": note.Content, "version": note.Version, "modifiedAt": note.ModifiedAt}), nil
}

func (s *Server) listNotes(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		PathPrefix string `json:"pathPrefix"`
		Tag        string `json:"tag"`
		Type       string `json:"type"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	notes, err := s.service.ListNotes(ctx)
	if err != nil {
		return "", err
	}
	filtered := make([]application.NoteSummaryDTO, 0, len(notes))
	for _, n := range notes {
		if args.PathPrefix != "" && !strings.HasPrefix(n.ID, args.PathPrefix) && !strings.HasPrefix(n.Path, args.PathPrefix) {
			continue
		}
		if args.Tag != "" && !containsTag(n.Tags, args.Tag) {
			continue
		}
		if args.Type != "" && !strings.EqualFold(n.Type, args.Type) {
			continue
		}
		filtered = append(filtered, n)
	}
	return jsonString(map[string]any{"notes": filtered, "count": len(filtered)}), nil
}

func (s *Server) createNote(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	note, err := s.service.CreateNote(ctx, application.CreateNoteRequest{ID: args.ID, Content: args.Content, Mode: args.Mode})
	if err != nil {
		return "", err
	}
	return jsonString(map[string]any{"id": note.ID, "path": note.Path, "version": note.Version}), nil
}

func (s *Server) editNote(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID          string `json:"id"`
		Content     string `json:"content"`
		BaseVersion string `json:"base_version"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	note, err := s.service.SaveNote(ctx, application.SaveNoteRequest{ID: args.ID, Content: args.Content, BaseVersion: args.BaseVersion})
	if err != nil {
		var appErr application.AppError
		if errors.As(err, &appErr) && appErr.Code == application.ErrExternalConflict {
			return "", fmt.Errorf("conflict: note changed since base_version (current version %s) — re-read and retry", appErr.Detail)
		}
		return "", err
	}
	return jsonString(map[string]any{"id": note.ID, "version": note.Version}), nil
}

func (s *Server) uploadAsset(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID         string `json:"id"`
		FileName   string `json:"file_name"`
		MIMEType   string `json:"mime_type"`
		DataBase64 string `json:"data_base64"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	resp, err := s.service.SaveNoteAsset(ctx, application.SaveNoteAssetRequest{
		NoteID:     args.ID,
		FileName:   args.FileName,
		MIMEType:   args.MIMEType,
		DataBase64: args.DataBase64,
	})
	if err != nil {
		return "", err
	}
	// Return the stored path and the Markdown tag; the agent inserts the tag into
	// the note body via edit_note (upload does not modify note content itself).
	return jsonString(map[string]any{"path": resp.Path, "markdown": resp.Markdown}), nil
}

func (s *Server) backlinks(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	links, err := s.service.Backlinks(ctx, args.ID)
	if err != nil {
		return "", err
	}
	return jsonString(map[string]any{"backlinks": links, "count": len(links)}), nil
}

func (s *Server) neighborhood(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		ID    string `json:"id"`
		Depth int    `json:"depth"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Depth <= 0 {
		args.Depth = 1
	}
	graph, err := s.service.Neighborhood(ctx, args.ID, args.Depth)
	if err != nil {
		return "", err
	}
	return jsonString(graph), nil
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}
