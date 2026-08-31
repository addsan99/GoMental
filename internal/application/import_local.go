package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"GoMental/internal/domain"
	"GoMental/internal/okf"
)

// importableTextExtensions is an allowlist rather than a blocklist because the
// failure modes are asymmetric: refusing a file the user wanted costs one clear
// error message, whereas accepting a binary writes garbage into the workspace
// and pollutes the search index until someone notices.
var importableTextExtensions = map[string]struct{}{
	".md": {}, ".markdown": {}, ".mdown": {}, ".mkd": {}, ".mdx": {},
	".txt": {}, ".text": {}, ".rst": {}, ".org": {}, ".adoc": {}, ".asciidoc": {},
}

const maxLocalImportBytes = 8 * 1024 * 1024

// looksLikeLocalImportPath decides whether the single import field holds a
// filesystem path instead of a URL. The rule is deliberately conservative: a
// bare relative path like "notes/a.md" is ambiguous (relative to the app's
// working directory, which the user cannot see), so a path must be absolute,
// home-relative, explicitly dot-relative, or a file:// URL.
func looksLikeLocalImportPath(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "file://") {
		return true
	}
	return strings.HasPrefix(trimmed, "/") ||
		strings.HasPrefix(trimmed, "~") ||
		strings.HasPrefix(trimmed, "./") ||
		strings.HasPrefix(trimmed, "../")
}

// resolveLocalImportPath turns the raw field into an absolute path.
func resolveLocalImportPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "file://")
	if trimmed == "" {
		return "", errors.New("empty path")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(trimmed, "~"), "/"))
	}
	return filepath.Abs(trimmed)
}

// readImportableTextFile validates and reads a local file. Both checks matter:
// the extension check catches the common mistake early with a message naming the
// problem, and the content check catches a binary that was merely renamed.
func readImportableTextFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no file at %s", path)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxLocalImportBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxLocalImportBytes)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := importableTextExtensions[ext]; !ok {
		return "", fmt.Errorf("%q is not a supported text file extension", ext)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("file is not valid UTF-8 text")
	}
	// A NUL byte is valid UTF-8 but never appears in prose; it is the cheapest
	// reliable tell that a file with a text extension is actually binary.
	for _, b := range data {
		if b == 0 {
			return "", errors.New("file contains binary data")
		}
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), nil
}

// importLocalFile mirrors ImportURL's save/project/emit tail for a file on disk.
func (s *Service) importLocalFile(ctx context.Context, req ImportURLRequest) (NoteDTO, error) {
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	repo, searchIndex, graphStore, err := s.sessionSnapshot()
	if err != nil {
		return NoteDTO{}, err
	}
	path, err := resolveLocalImportPath(req.URL)
	if err != nil {
		return NoteDTO{}, appErr("import.invalid_path", "Could not resolve the file path", err)
	}
	raw, err := readImportableTextFile(path)
	if err != nil {
		return NoteDTO{}, appErr("import.invalid_file", "Could not import that file", err)
	}
	content, title, err := localImportDocument(path, raw)
	if err != nil {
		return NoteDTO{}, appErr("import.failed", "Could not build a note from that file", err)
	}

	noteID := domain.NoteID(localImportSlug(path, title))
	noteID, err = ws.QualifyNewNoteIDIn(noteID, req.Member)
	if err != nil {
		return NoteDTO{}, appErr("import.note_id_failed", "Could not place the imported note in a member workspace", err)
	}
	noteID, err = uniqueImportedNoteID(ctx, repo, noteID)
	if err != nil {
		return NoteDTO{}, appErr("import.note_id_failed", "Could not choose an import note ID", err)
	}

	unlock := s.lockNote(noteID)
	defer unlock()

	note := domain.Note{ID: noteID, Document: domain.OKFDocument{Raw: s.prepareNewNoteContent(ctx, ws, noteID, content)}}
	if err := repo.Save(ctx, note); err != nil {
		return NoteDTO{}, appErr("import.save_failed", "Could not save imported note", err)
	}
	read, err := repo.Read(ctx, noteID)
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

// localImportDocument returns the note content and its title. A file that already
// carries usable frontmatter is imported as-is — rewriting it would throw away
// metadata the user deliberately wrote — otherwise one is synthesized around the
// file's text.
func localImportDocument(path, raw string) (string, string, error) {
	if parsed, err := okf.NewCodec().Decode(domain.NoteID("import"), raw, time.Now()); err == nil {
		if _, _, hasFrontmatter := rawFrontmatter(raw); hasFrontmatter && strings.TrimSpace(parsed.Metadata.Type) != "" {
			title := firstNonBlank(parsed.Title, headingTitle(raw), fileTitle(path))
			return raw, title, nil
		}
	}
	title := firstNonBlank(headingTitle(raw), fileTitle(path))
	body := raw
	if !strings.HasPrefix(strings.TrimSpace(body), "#") {
		body = "# " + title + "\n\n" + strings.TrimLeft(body, "\n")
	}
	timestamp := time.Now().UTC()
	document, err := okf.NewCodec().Encode(domain.OKFMetadata{
		Type:      "general",
		Title:     title,
		Timestamp: &timestamp,
		Unknown:   map[string]any{"source_path": path},
	}, body)
	if err != nil {
		return "", "", err
	}
	return document.Raw, title, nil
}

// headingTitle returns the text of the file's first ATX heading, if any.
func headingTitle(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if text != "" {
			return text
		}
	}
	return ""
}

// fileTitle turns "my-meeting_notes.md" into "My meeting notes".
func fileTitle(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(base)
	fields := strings.Fields(base)
	if len(fields) == 0 {
		return "Imported note"
	}
	joined := strings.Join(fields, " ")
	return strings.ToUpper(joined[:1]) + joined[1:]
}

func localImportSlug(path, title string) string {
	if s := importSlug(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))); s != "" {
		return s
	}
	if s := importSlug(title); s != "" {
		return s
	}
	return "imported-note"
}

func importSlug(raw string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
