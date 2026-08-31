package application

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"GoMental/internal/composite"
	"GoMental/internal/domain"
	"GoMental/internal/okf"
	"GoMental/internal/workspace"
)

// Auto-tagging proposes tags for a note that has none, drawing *only* from the
// tags the workspace already uses. That restriction is the whole design: a
// generated tag that nobody else uses is a singleton that fragments the tag
// namespace and helps no query, whereas re-using an existing tag immediately
// connects the note to its neighbours in search, facets and the graph. So this
// is a recall problem over a fixed vocabulary, not a keyword-invention problem.
//
// Every proposal carries a confidence and only proposals above autoTagMinConfidence
// are written, because a wrong tag is worse than a missing one: a missing tag is
// invisible, a wrong tag actively pollutes filters and has to be found and undone.
const (
	// A body-only mention scores below this, so a tag needs a title hit, a heading
	// hit, or sustained repetition in the body to be written. See tagFieldWeight.
	autoTagMinConfidence = 0.62
	// Notes end up unreadable past a handful of tags, and the tail of a ranked
	// list is exactly where the false positives live.
	autoTagMaxTags = 5
	// Below this length a token matches too much by accident ("ai", "go", "ui"),
	// so short tags are only accepted on the strongest signal (the title).
	autoTagShortTagRunes = 3
	// A tag carried by most of the workspace says nothing about any single note.
	autoTagCommonRatio = 0.60
	autoTagUselessRatio = 0.85
)

// Field weights. A title mention is close to a declaration of subject; a heading
// mention marks a section of the note; a body mention is weak on its own.
const (
	tagWeightTitle   = 0.90
	tagWeightHeading = 0.72
	tagWeightBody    = 0.50
)

var (
	fencedCodeBlock = regexp.MustCompile("(?s)```.*?```")
	inlineCode      = regexp.MustCompile("`[^`]*`")
	nonWordRun      = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// autoTagInput is the note-side half of scoring, kept free of domain types so the
// scorer stays a pure function that tests can drive directly.
type autoTagInput struct {
	Title    string
	Headings []string
	Body     string
}

type scoredTag struct {
	Tag        string
	Confidence float64
}

func autoTagInputFrom(note domain.ParsedOKFNote) autoTagInput {
	headings := make([]string, 0, len(note.Headings))
	for _, heading := range note.Headings {
		headings = append(headings, heading.Text)
	}
	return autoTagInput{Title: note.Title, Headings: headings, Body: note.Body}
}

// suggestTags ranks the workspace vocabulary against one note. corpusSize is the
// number of notes the vocabulary counts were taken over; it is only used to
// discount ubiquitous tags, so a zero (or absurd) value just disables that
// discount rather than distorting the scores.
func suggestTags(input autoTagInput, vocabulary map[string]int, corpusSize int) []scoredTag {
	if len(vocabulary) == 0 {
		return nil
	}
	title := tagSearchText(input.Title)
	headings := tagSearchText(strings.Join(input.Headings, "\n"))
	body := tagSearchText(stripCode(input.Body))

	out := make([]scoredTag, 0, autoTagMaxTags)
	for tag, noteCount := range vocabulary {
		normalized := okf.NormalizeTag(tag)
		if normalized == "" {
			continue
		}
		needle := tagSearchText(normalized)
		if needle == "" {
			continue
		}
		short := len([]rune(strings.ReplaceAll(needle, " ", ""))) < autoTagShortTagRunes

		confidence := 0.0
		switch {
		case countPhrase(title, needle) > 0:
			confidence = tagWeightTitle
		case short:
			// Not in the title and too short to trust anywhere else.
			continue
		case countPhrase(headings, needle) > 0:
			confidence = tagWeightHeading
		default:
			hits := countPhrase(body, needle)
			if hits == 0 {
				continue
			}
			// Repetition is the only evidence a body-only match has. Cap it so no
			// amount of repetition can substitute for a title or heading.
			confidence = tagWeightBody + minFloat(0.12, 0.03*float64(hits-1))
		}

		if corpusSize > 0 {
			ratio := float64(noteCount) / float64(corpusSize)
			switch {
			case ratio >= autoTagUselessRatio:
				continue
			case ratio >= autoTagCommonRatio:
				confidence *= 0.5
			}
		}
		if confidence < autoTagMinConfidence {
			continue
		}
		out = append(out, scoredTag{Tag: normalized, Confidence: confidence})
	}
	// Confidence first, then alphabetical so the result is deterministic — map
	// iteration order would otherwise make the written frontmatter unstable.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Tag < out[j].Tag
	})
	if len(out) > autoTagMaxTags {
		out = out[:autoTagMaxTags]
	}
	return out
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// stripCode removes fenced and inline code so a tag is not "found" in a snippet
// that merely happens to contain the word.
func stripCode(body string) string {
	body = fencedCodeBlock.ReplaceAllString(body, " ")
	return inlineCode.ReplaceAllString(body, " ")
}

// tagSearchText reduces text to space-separated lowercase word tokens, padded with
// a leading and trailing space. Padding is what makes a plain strings.Index give
// whole-word matching: searching for " go " can never hit inside "golang".
func tagSearchText(text string) string {
	cleaned := nonWordRun.ReplaceAllString(strings.ToLower(text), " ")
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return ""
	}
	return " " + strings.Join(fields, " ") + " "
}

// countPhrase counts non-overlapping whole-word occurrences of needle in haystack.
// Both must already be tagSearchText-normalized.
func countPhrase(haystack, needle string) int {
	if haystack == "" || needle == "" {
		return 0
	}
	// Both sides are padded, so the shared space has to be dropped from one of
	// them or " go " would never match at the very start of " go is fast ".
	phrase := needle[1:]
	count := 0
	offset := 0
	for {
		idx := strings.Index(haystack[offset:], phrase)
		if idx < 0 {
			return count
		}
		count++
		offset += idx + len(phrase) - 1
	}
}

// autoTagNote fills in tags for a note that has none, honouring the workspace
// setting. It returns the (possibly rewritten) raw note and whether it changed.
// Failures are deliberately swallowed into "no change": auto-tagging is a
// convenience, and it must never be the reason a save fails.
func (s *Service) autoTagNote(ctx context.Context, ws workspace.Workspace, id domain.NoteID, raw string) (string, bool) {
	if !s.autoTagEnabled(ctx, ws.Root()) {
		return raw, false
	}
	parsed, err := composite.Decode(ws, id, raw, time.Time{})
	if err != nil || len(parsed.Tags) > 0 {
		return raw, false
	}
	_, _, graphStore, err := s.sessionSnapshot()
	if err != nil || graphStore == nil {
		return raw, false
	}
	vocabulary, err := graphStore.TagVocabulary(ctx)
	if err != nil || len(vocabulary) == 0 {
		return raw, false
	}
	corpusSize, err := graphStore.CountNotes(ctx)
	if err != nil {
		corpusSize = 0
	}
	scored := suggestTags(autoTagInputFrom(parsed), vocabulary, corpusSize)
	if len(scored) == 0 {
		return raw, false
	}
	tags := make([]string, 0, len(scored))
	for _, item := range scored {
		tags = append(tags, item.Tag)
	}
	return setTagsFrontmatter(raw, tags)
}

// autoTagEnabled resolves the workspace-level toggle. It defaults to on, so a
// missing settings file or an unreadable one still gets the feature.
func (s *Service) autoTagEnabled(ctx context.Context, root string) bool {
	settings, err := s.LoadSettings(ctx)
	if err != nil {
		return true
	}
	ws, ok := settings.Workspaces[root]
	if !ok {
		return true
	}
	return normalizeAutoTag(ws.AutoTag) == autoTagOn
}

// prepareNewNoteContent is the write-path guarantee behind "every new or imported
// note has a tags array": auto-tag first (so a confident tag lands in the array
// rather than beside it), then make sure the key exists even when nothing was
// found, so an editor can add a tag without hand-writing the YAML.
// applyAutoTags is the save-path hook: it fills in tags for a note that has none
// and otherwise returns the content untouched. It never adds frontmatter to a
// note that has none — an ordinary save is not the moment to restructure a file
// the user did not ask to restructure.
func (s *Service) applyAutoTags(ctx context.Context, ws workspace.Workspace, id domain.NoteID, raw string) string {
	tagged, _ := s.autoTagNote(ctx, ws, id, raw)
	return tagged
}

func (s *Service) prepareNewNoteContent(ctx context.Context, ws workspace.Workspace, id domain.NoteID, raw string) string {
	tagged, _ := s.autoTagNote(ctx, ws, id, raw)
	ensured, _ := ensureTagsFrontmatter(tagged)
	return ensured
}

// ensureTagsFrontmatter adds an empty tags array when the note has none. A note
// with no frontmatter at all gets one, since this only runs on notes being
// created or imported.
func ensureTagsFrontmatter(raw string) (string, bool) {
	frontmatter, suffix, ok := rawFrontmatter(raw)
	if !ok {
		return "---\ntags: []\n---\n" + raw, true
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		if isTagsKeyLine(line) {
			return raw, false
		}
	}
	return "---\n" + strings.TrimRight(frontmatter, "\n") + "\ntags: []" + suffix, true
}

// setTagsFrontmatter writes tags into the note's frontmatter. It is only called
// for notes whose parsed tags are empty, so an existing tags key is either bare
// or an empty array and can be replaced outright — along with any block sequence
// items indented beneath it, which would otherwise be orphaned under the new
// flow-style value.
func setTagsFrontmatter(raw string, tags []string) (string, bool) {
	if len(tags) == 0 {
		return raw, false
	}
	value := "tags: [" + strings.Join(quoteTagsForYAML(tags), ", ") + "]"
	frontmatter, suffix, ok := rawFrontmatter(raw)
	if !ok {
		return "---\n" + value + "\n---\n" + raw, true
	}
	lines := strings.Split(frontmatter, "\n")
	out := make([]string, 0, len(lines)+1)
	replaced := false
	for i := 0; i < len(lines); i++ {
		if !isTagsKeyLine(lines[i]) {
			out = append(out, lines[i])
			continue
		}
		out = append(out, value)
		replaced = true
		for i+1 < len(lines) && isBlockSequenceItem(lines[i+1]) {
			i++
		}
	}
	if !replaced {
		out = append(out, value)
	}
	return "---\n" + strings.Join(out, "\n") + suffix, true
}

func isTagsKeyLine(line string) bool {
	trimmed := strings.TrimRight(line, " \t")
	return trimmed == "tags:" || strings.HasPrefix(trimmed, "tags: ") || strings.HasPrefix(trimmed, "tags:\t")
}

func isBlockSequenceItem(line string) bool {
	if line == "" || (line[0] != ' ' && line[0] != '\t') {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(line), "-")
}

var plainYAMLTag = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_./-]*$`)

// quoteTagsForYAML keeps flow-style output valid. Normalized tags are almost
// always plain scalars, but a tag inherited from hand-written frontmatter can
// carry a comma or bracket that would silently change the parse.
func quoteTagsForYAML(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if plainYAMLTag.MatchString(tag) {
			out = append(out, tag)
			continue
		}
		out = append(out, `"`+strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(tag)+`"`)
	}
	return out
}
