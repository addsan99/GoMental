// Composite workspaces present several member workspaces as one. The composite
// root holds only metadata (its own graph and search projections); every note
// still lives in, and is written back to, the member that owns it.
//
// A member is itself an ordinary Workspace, so member-scoped work reuses the
// single-workspace logic unchanged. The composite's only job is to translate
// between its namespaced note IDs ("<prefix>/<member note id>") and the member
// that answers for them.

package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"GoMental/internal/domain"
	"GoMental/internal/ingest"

	"gopkg.in/yaml.v3"
)

// CompositeFileName is the config that turns a directory into a composite workspace.
var CompositeFileName = filepath.Join(".gomental", "composite.yaml")

var (
	ErrCompositeNested     = errors.New("composite workspaces cannot contain another composite")
	ErrUnknownNotePrefix   = errors.New("note id does not belong to any composite member")
	ErrCompositeReadOnlyOp = errors.New("operation is not supported on a composite workspace")
)

// Member is one workspace taking part in a composite.
type Member struct {
	// Prefix is the first path segment of every note ID this member owns.
	Prefix string
	ws     Workspace
}

func (m Member) Root() string            { return m.ws.Root() }
func (m Member) Workspace() Workspace    { return m.ws }
func (m Member) Mapping() ingest.Mapping { return m.ws.Mapping() }

type compositeConfig struct {
	Members []compositeMemberConfig `yaml:"members"`
}

type compositeMemberConfig struct {
	Prefix string `yaml:"prefix"`
	Root   string `yaml:"root"`
}

// IsComposite reports whether a directory is configured as a composite workspace.
// It is deliberately cheap: it only looks for the config file, so it can be used
// to reject nesting before paying for a full open.
func IsComposite(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, CompositeFileName))
	return err == nil
}

// WriteCompositeConfig makes root a composite over the given member roots,
// assigning each a stable, human-readable prefix. Existing prefixes are kept so
// that note IDs — and therefore saved layouts and starred notes — survive edits
// to the member list.
func WriteCompositeConfig(root string, memberRoots []string) error {
	existing := map[string]string{}
	if cfg, err := readCompositeConfig(root); err == nil {
		for _, m := range cfg.Members {
			existing[m.Root] = m.Prefix
		}
	}

	used := map[string]bool{}
	cfg := compositeConfig{}
	for _, raw := range memberRoots {
		abs, err := filepath.Abs(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidWorkspaceRoot, err)
		}
		abs = filepath.Clean(abs)
		if IsComposite(abs) {
			return fmt.Errorf("%w: %s", ErrCompositeNested, abs)
		}
		prefix := existing[abs]
		if prefix == "" || used[prefix] {
			prefix = uniquePrefix(abs, used)
		}
		used[prefix] = true
		cfg.Members = append(cfg.Members, compositeMemberConfig{Prefix: prefix, Root: abs})
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	path := filepath.Join(root, CompositeFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// DefaultCompositeRoot is the app-managed directory that hosts the single
// composite workspace. The composite holds only projections, so it does not
// belong among the user's own note directories.
func DefaultCompositeRoot() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "GoMental", "composite"), nil
}

// CompositeMemberRoots returns the member roots configured for a composite root,
// in order, without opening them.
func CompositeMemberRoots(root string) ([]string, error) {
	cfg, err := readCompositeConfig(root)
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(cfg.Members))
	for _, m := range cfg.Members {
		roots = append(roots, m.Root)
	}
	return roots, nil
}

// CompositePrefixFor reports the prefix a composite assigns to a member root,
// or "" when that root is not a member.
func CompositePrefixFor(root, memberRoot string) string {
	cfg, err := readCompositeConfig(root)
	if err != nil {
		return ""
	}
	target := filepath.Clean(memberRoot)
	for _, member := range cfg.Members {
		if filepath.Clean(member.Root) == target {
			return member.Prefix
		}
	}
	return ""
}

func readCompositeConfig(root string) (compositeConfig, error) {
	raw, err := os.ReadFile(filepath.Join(root, CompositeFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return compositeConfig{}, nil
		}
		return compositeConfig{}, err
	}
	var cfg compositeConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return compositeConfig{}, err
	}
	return cfg, nil
}

// openMembers resolves the configured members. A member root that has gone
// missing is skipped rather than failing the whole composite: an unplugged
// volume should cost you that member's notes, not the workspace.
func openMembers(root, metadataDir string) ([]Member, error) {
	cfg, err := readCompositeConfig(root)
	if err != nil {
		return nil, err
	}
	members := make([]Member, 0, len(cfg.Members))
	used := map[string]bool{}
	for _, entry := range cfg.Members {
		memberRoot := filepath.Clean(strings.TrimSpace(entry.Root))
		if memberRoot == "" || memberRoot == "." {
			continue
		}
		if IsComposite(memberRoot) {
			return nil, fmt.Errorf("%w: %s", ErrCompositeNested, memberRoot)
		}
		ws, err := OpenWithMetadataDir(memberRoot, metadataDir)
		if err != nil {
			continue
		}
		prefix := sanitizePrefix(entry.Prefix)
		if prefix == "" || used[prefix] {
			prefix = uniquePrefix(memberRoot, used)
		}
		used[prefix] = true
		members = append(members, Member{Prefix: prefix, ws: ws})
	}
	sort.SliceStable(members, func(i, j int) bool { return members[i].Prefix < members[j].Prefix })
	return members, nil
}

func uniquePrefix(root string, used map[string]bool) string {
	base := sanitizePrefix(filepath.Base(root))
	if base == "" {
		base = "workspace"
	}
	candidate := base
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return candidate
}

func sanitizePrefix(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// IsComposite reports whether this workspace aggregates member workspaces.
func (w Workspace) IsComposite() bool { return len(w.members) > 0 }

// Members returns the composite's members (empty for an ordinary workspace).
func (w Workspace) Members() []Member { return w.members }

// MemberForNoteID resolves the member that owns a namespaced note ID, along with
// the ID as that member knows it.
func (w Workspace) MemberForNoteID(id domain.NoteID) (Member, domain.NoteID, error) {
	prefix, rest, ok := strings.Cut(strings.TrimPrefix(filepath.ToSlash(string(id)), "/"), "/")
	if !ok || rest == "" {
		return Member{}, "", fmt.Errorf("%w: %s", ErrUnknownNotePrefix, id)
	}
	for _, member := range w.members {
		if member.Prefix == prefix {
			normalized, err := member.ws.NormalizeNoteID(rest)
			if err != nil {
				return Member{}, "", err
			}
			return member, normalized, nil
		}
	}
	return Member{}, "", fmt.Errorf("%w: %s", ErrUnknownNotePrefix, id)
}

// MemberForPath resolves the member that owns an absolute filesystem path.
func (w Workspace) MemberForPath(path string) (Member, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Member{}, false
	}
	abs = filepath.Clean(abs)
	for _, member := range w.members {
		rel, err := filepath.Rel(member.Root(), abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return member, true
	}
	return Member{}, false
}

// MappingForNoteID returns the ingest profile that should be used to parse a
// note. On a composite this varies per note, because members can be ingested
// from different sources.
func (w Workspace) MappingForNoteID(id domain.NoteID) ingest.Mapping {
	if !w.IsComposite() {
		return w.mapping
	}
	member, _, err := w.MemberForNoteID(id)
	if err != nil {
		return ingest.Mapping{}
	}
	return member.Mapping()
}

// NamespaceNoteID prefixes a member-scoped note ID for use in the composite.
func (m Member) NamespaceNoteID(id domain.NoteID) domain.NoteID {
	return domain.NoteID(m.Prefix + "/" + filepath.ToSlash(string(id)))
}

// QualifyNewNoteID places a freshly minted note ID inside the composite. An ID
// that already names a member is left alone; anything else lands in the first
// member, which is the one the composite lists first and so the one a user is
// most likely to mean by "here".
func (w Workspace) QualifyNewNoteID(id domain.NoteID) (domain.NoteID, error) {
	if !w.IsComposite() {
		return id, nil
	}
	if _, _, err := w.MemberForNoteID(id); err == nil {
		return id, nil
	}
	if len(w.members) == 0 {
		return "", fmt.Errorf("%w: composite has no available members", ErrUnknownNotePrefix)
	}
	return w.members[0].NamespaceNoteID(id), nil
}
