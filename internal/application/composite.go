package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"GoMental/internal/workspace"
)

// CompositeMemberDTO is one workspace taking part in the composite.
type CompositeMemberDTO struct {
	Root    string `json:"root"`
	Name    string `json:"name"`
	Prefix  string `json:"prefix"`
	Missing bool   `json:"missing"`
}

// CompositeDTO describes the single composite workspace the app manages.
type CompositeDTO struct {
	Root       string               `json:"root"`
	Configured bool                 `json:"configured"`
	Members    []CompositeMemberDTO `json:"members"`
}

// Composite returns the current composite definition, creating nothing.
func (s *Service) Composite(ctx context.Context) (CompositeDTO, error) {
	root, err := workspace.DefaultCompositeRoot()
	if err != nil {
		return CompositeDTO{}, appErr("composite.unavailable", "Could not locate the composite workspace", err)
	}
	roots, err := workspace.CompositeMemberRoots(root)
	if err != nil {
		return CompositeDTO{}, appErr("composite.read_failed", "Could not read the composite workspace", err)
	}
	return compositeDTO(root, roots), nil
}

// SaveComposite redefines the composite's members.
//
// Rebuilding is left to the next open: the projections are derived data, and
// rebuilding here would make editing the member list feel like an import.
func (s *Service) SaveComposite(ctx context.Context, memberRoots []string) (CompositeDTO, error) {
	root, err := workspace.DefaultCompositeRoot()
	if err != nil {
		return CompositeDTO{}, appErr("composite.unavailable", "Could not locate the composite workspace", err)
	}
	cleaned := make([]string, 0, len(memberRoots))
	seen := map[string]bool{}
	for _, raw := range memberRoots {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		abs, err := filepath.Abs(trimmed)
		if err != nil {
			return CompositeDTO{}, appErr("composite.invalid_member", "Invalid workspace path", err)
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			continue
		}
		if abs == root {
			return CompositeDTO{}, appErr("composite.self_member", "The composite workspace cannot include itself", nil)
		}
		if workspace.IsComposite(abs) {
			return CompositeDTO{}, appErr("composite.nested", "A composite workspace cannot include another composite workspace", nil)
		}
		if _, err := workspace.Open(abs); err != nil {
			return CompositeDTO{}, appErr("composite.invalid_member", "Could not open workspace "+abs, err)
		}
		seen[abs] = true
		cleaned = append(cleaned, abs)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return CompositeDTO{}, appErr("composite.write_failed", "Could not create the composite workspace", err)
	}
	if err := workspace.WriteCompositeConfig(root, cleaned); err != nil {
		return CompositeDTO{}, appErr("composite.write_failed", "Could not save the composite workspace", err)
	}
	return compositeDTO(root, cleaned), nil
}

func compositeDTO(root string, memberRoots []string) CompositeDTO {
	members := make([]CompositeMemberDTO, 0, len(memberRoots))
	for _, memberRoot := range memberRoots {
		members = append(members, CompositeMemberDTO{
			Root:    memberRoot,
			Name:    filepath.Base(memberRoot),
			Prefix:  workspace.CompositePrefixFor(root, memberRoot),
			Missing: !workspace.RootExists(memberRoot),
		})
	}
	return CompositeDTO{Root: root, Configured: len(members) > 0, Members: members}
}
