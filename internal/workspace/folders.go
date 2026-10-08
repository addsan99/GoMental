package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizeFolder cleans a workspace-relative folder path.
//
// The empty string is valid and means the workspace root: in the note tree a
// note filed at the top level has no folder segment at all, so "no folder" has
// to be expressible rather than being an error.
func (w Workspace) NormalizeFolder(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return "", fmt.Errorf("%w: absolute path", ErrInvalidFolder)
	}
	if len(raw) >= 3 && isWindowsDrivePrefix(raw) {
		return "", fmt.Errorf("%w: absolute path", ErrInvalidFolder)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("%w: traversal", ErrInvalidFolder)
	}
	return strings.Trim(clean, "/"), nil
}

// PathForFolder resolves a workspace-relative folder to an absolute path.
//
// On a composite the leading segment names a member, and resolves into that
// member's own root rather than a directory inside the composite — the
// composite root is a container for the member list and holds no notes itself.
func (w Workspace) PathForFolder(folder string) (string, error) {
	clean, err := w.NormalizeFolder(folder)
	if err != nil {
		return "", err
	}
	if w.IsComposite() {
		if clean == "" {
			return w.root, nil
		}
		prefix, rest, _ := strings.Cut(clean, "/")
		for _, member := range w.members {
			if member.Prefix == prefix {
				return member.ws.PathForFolder(rest)
			}
		}
		return "", fmt.Errorf("%w: %s", ErrUnknownNotePrefix, folder)
	}
	if clean == "" {
		return w.root, nil
	}
	abs := filepath.Join(w.root, filepath.FromSlash(clean))
	if err := w.ensureInside(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// ValidFolderName reports whether a single path segment is usable as a new
// folder name.
//
// Names that the note scanner would skip are rejected rather than accepted and
// silently ignored: a folder the user cannot then see or file anything into is
// worse than a refusal. That covers dot-folders, which are hidden by the OS
// file managers too, and the "~" prefix the scanner treats as scratch.
func ValidFolderName(name string) error {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return fmt.Errorf("%w: empty name", ErrInvalidFolder)
	case strings.ContainsAny(trimmed, `/\`):
		return fmt.Errorf("%w: name cannot contain a path separator", ErrInvalidFolder)
	case trimmed == "." || trimmed == "..":
		return fmt.Errorf("%w: reserved name", ErrInvalidFolder)
	case strings.HasPrefix(trimmed, "."):
		return fmt.Errorf("%w: a name starting with “.” is hidden", ErrInvalidFolder)
	case strings.HasPrefix(trimmed, "~"):
		return fmt.Errorf("%w: a name starting with “~” is ignored", ErrInvalidFolder)
	case strings.EqualFold(trimmed, "node_modules"):
		return fmt.Errorf("%w: reserved name", ErrInvalidFolder)
	}
	return nil
}

// CreateFolder makes a new empty directory inside an existing folder and
// returns its workspace-relative path.
//
// The parent has to exist already. Folders in the tree are derived from the
// notes filed under them, so every folder the user can right-click is on disk;
// creating missing ancestors here would only mask a stale or mistyped parent.
func (w Workspace) CreateFolder(parent, name string) (string, error) {
	if err := ValidFolderName(name); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	cleanParent, err := w.NormalizeFolder(parent)
	if err != nil {
		return "", err
	}
	if w.IsComposite() && cleanParent == "" {
		return "", fmt.Errorf("%w: choose a workspace member to create the folder in", ErrInvalidFolder)
	}
	parentPath, err := w.PathForFolder(cleanParent)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(parentPath)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidFolder, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: parent is not a directory", ErrInvalidFolder)
	}
	target := filepath.Join(parentPath, name)
	if strings.EqualFold(name, w.metadataDir) {
		return "", fmt.Errorf("%w: %s is reserved", ErrInvalidFolder, w.metadataDir)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: %s", ErrFolderAlreadyExists, name)
		}
		return "", err
	}
	if cleanParent == "" {
		return name, nil
	}
	return cleanParent + "/" + name, nil
}
