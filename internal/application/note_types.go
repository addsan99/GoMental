package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// NoteTypeDTO is a workspace-owned note definition. The template is Markdown
// with {{title}}, {{titleYaml}}, {{id}}, {{type}}, and {{date}} placeholders.
type NoteTypeDTO struct {
	ID          string `json:"id" yaml:"id"`
	Label       string `json:"label" yaml:"label"`
	Description string `json:"description" yaml:"description,omitempty"`
	Template    string `json:"template" yaml:"template"`
	Source      string `json:"source" yaml:"-"`
}

type NoteTypeCollectionDTO struct {
	Name  string        `json:"name" yaml:"name"`
	Types []NoteTypeDTO `json:"types" yaml:"types"`
}

var noteTypeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (s *Service) ListNoteTypes(ctx context.Context) ([]NoteTypeDTO, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.typeRootLocked()
	if err != nil {
		return nil, err
	}
	if err := ensureBuiltinTypes(root); err != nil {
		return nil, appErr("types.initialize_failed", "Could not initialize workspace note types", err)
	}
	if err := upgradeGenericStarterTypes(root); err != nil {
		return nil, appErr("types.initialize_failed", "Could not upgrade workspace note types", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, appErr("types.read_failed", "Could not read workspace note types", err)
	}
	types := builtinNoteTypes()
	builtinIDs := builtinNoteTypeIDs()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, appErr("types.read_failed", "Could not read workspace note type", err)
		}
		var definition NoteTypeDTO
		if err := yaml.Unmarshal(data, &definition); err != nil {
			return nil, appErr("types.invalid", fmt.Sprintf("Could not parse note type %s", entry.Name()), err)
		}
		if err := validateNoteType(definition); err != nil {
			return nil, appErr("types.invalid", fmt.Sprintf("Invalid note type %s", entry.Name()), err)
		}
		// Built-ins cannot be overridden by workspace files. concept.yaml is
		// ignored as a migration path from the former Concept note type.
		if _, builtin := builtinIDs[definition.ID]; builtin || definition.ID == "concept" {
			continue
		}
		definition.Source = "workspace"
		types = append(types, definition)
	}
	sort.Slice(types, func(i, j int) bool { return strings.ToLower(types[i].Label) < strings.ToLower(types[j].Label) })
	return types, nil
}

func (s *Service) SaveNoteType(ctx context.Context, definition NoteTypeDTO) (NoteTypeDTO, error) {
	if err := ctx.Err(); err != nil {
		return NoteTypeDTO{}, err
	}
	definition.ID = strings.TrimSpace(strings.ToLower(definition.ID))
	definition.Label = strings.TrimSpace(definition.Label)
	definition.Description = strings.TrimSpace(definition.Description)
	definition.Source = "workspace"
	if _, builtin := builtinNoteTypeIDs()[definition.ID]; builtin || definition.ID == "concept" {
		return NoteTypeDTO{}, appErr("types.builtin", "Built-in note types cannot be changed", nil)
	}
	if err := validateNoteType(definition); err != nil {
		return NoteTypeDTO{}, appErr("types.invalid", "Invalid note type", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.typeRootLocked()
	if err != nil {
		return NoteTypeDTO{}, err
	}
	if err := ensureBuiltinTypes(root); err != nil {
		return NoteTypeDTO{}, appErr("types.initialize_failed", "Could not initialize workspace note types", err)
	}
	encoded, err := marshalNoteType(definition)
	if err != nil {
		return NoteTypeDTO{}, appErr("types.encode_failed", "Could not encode note type", err)
	}
	if err := os.WriteFile(filepath.Join(root, definition.ID+".yaml"), encoded, 0o644); err != nil {
		return NoteTypeDTO{}, appErr("types.write_failed", "Could not save note type", err)
	}
	return definition, nil
}

func (s *Service) DeleteNoteType(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id = strings.TrimSpace(strings.ToLower(id))
	if !noteTypeID.MatchString(id) {
		return appErr("types.invalid", "Invalid note type ID", nil)
	}
	if _, builtin := builtinNoteTypeIDs()[id]; builtin || id == "concept" {
		return appErr("types.builtin", "Built-in note types cannot be removed", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.typeRootLocked()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, id+".yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return appErr("types.delete_failed", "Could not delete note type", err)
	}
	return nil
}

// ImportNoteTypeCollection installs a downloaded collection into this workspace.
// Definitions are copied rather than linked, so later workspace edits stay local.
func (s *Service) ImportNoteTypeCollection(ctx context.Context, content string) ([]NoteTypeDTO, error) {
	var collection NoteTypeCollectionDTO
	if err := yaml.Unmarshal([]byte(content), &collection); err != nil {
		return nil, appErr("types.collection_invalid", "Could not parse note type collection", err)
	}
	if len(collection.Types) == 0 {
		return nil, appErr("types.collection_invalid", "A note type collection must contain at least one type", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.typeRootLocked()
	if err != nil {
		return nil, err
	}
	if err := ensureBuiltinTypes(root); err != nil {
		return nil, appErr("types.initialize_failed", "Could not initialize workspace note types", err)
	}
	seen := map[string]struct{}{}
	for index := range collection.Types {
		definition := &collection.Types[index]
		definition.ID = strings.TrimSpace(strings.ToLower(definition.ID))
		definition.Label = strings.TrimSpace(definition.Label)
		definition.Description = strings.TrimSpace(definition.Description)
		definition.Source = "workspace"
		if _, builtin := builtinNoteTypeIDs()[definition.ID]; builtin || definition.ID == "concept" {
			return nil, appErr("types.builtin", "Collections cannot replace built-in note types", nil)
		}
		if err := validateNoteType(*definition); err != nil {
			return nil, appErr("types.collection_invalid", "Invalid note type in collection", err)
		}
		if _, exists := seen[definition.ID]; exists {
			return nil, appErr("types.collection_invalid", "A collection cannot contain the same ID twice", nil)
		}
		seen[definition.ID] = struct{}{}
	}
	for _, definition := range collection.Types {
		encoded, err := marshalNoteType(definition)
		if err != nil {
			return nil, appErr("types.encode_failed", "Could not encode note type", err)
		}
		if err := os.WriteFile(filepath.Join(root, definition.ID+".yaml"), encoded, 0o644); err != nil {
			return nil, appErr("types.write_failed", "Could not import note type collection", err)
		}
	}
	sort.Slice(collection.Types, func(i, j int) bool {
		return strings.ToLower(collection.Types[i].Label) < strings.ToLower(collection.Types[j].Label)
	})
	return collection.Types, nil
}

func (s *Service) typeRootLocked() (string, error) {
	if s.repo == nil {
		return "", appErr(ErrWorkspaceInaccessible, "Open a workspace before managing note types", nil)
	}
	return filepath.Join(s.workspace.Root(), ".gomental", "types"), nil
}

func validateNoteType(definition NoteTypeDTO) error {
	if !noteTypeID.MatchString(definition.ID) {
		return errors.New("ID must use lowercase letters, numbers, and hyphens")
	}
	if definition.Label == "" {
		return errors.New("label is required")
	}
	if strings.TrimSpace(definition.Template) == "" {
		return errors.New("template is required")
	}
	return nil
}

func marshalNoteType(definition NoteTypeDTO) ([]byte, error) {
	return yaml.Marshal(struct {
		ID          string `yaml:"id"`
		Label       string `yaml:"label"`
		Description string `yaml:"description,omitempty"`
		Template    string `yaml:"template"`
	}{definition.ID, definition.Label, definition.Description, definition.Template})
}

func ensureBuiltinTypes(root string) error {
	marker := filepath.Join(root, ".initialized")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, definition := range starterWorkspaceNoteTypes() {
		path := filepath.Join(root, definition.ID+".yaml")
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		encoded, err := marshalNoteType(definition)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(marker, []byte("Workspace note types initialized by GoMental. Delete YAML files to remove types; keep this marker to prevent reseeding.\n"), 0o644)
}

func builtinNoteTypes() []NoteTypeDTO {
	items := []struct{ id, label, description string }{{"general", "General", "A note that does not fit another note type."}, {"term", "Term", "A term, definition, or mental model."}, {"how-to", "How-to", "A repeatable procedure."}, {"meeting", "Meeting", "A meeting record."}}
	out := make([]NoteTypeDTO, 0, len(items))
	for _, item := range items {
		out = append(out, NoteTypeDTO{ID: item.id, Label: item.label, Description: item.description, Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\n---\n\n# {{title}}\n\n## Notes\n\n", Source: "builtin"})
	}
	return out
}

func builtinNoteTypeIDs() map[string]struct{} {
	return map[string]struct{}{"general": {}, "term": {}, "how-to": {}, "meeting": {}}
}

func starterWorkspaceNoteTypes() []NoteTypeDTO {
	return []NoteTypeDTO{
		{ID: "adr", Label: "ADR", Description: "An architecture decision record.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\nstatus: proposed\ndate: {{date}}\nsuperseded_by:\n---\n\n# {{title}}\n\n## Context\n\n## Decision\n\n## Consequences\n\n## Alternatives considered\n"},
		{ID: "service", Label: "Service", Description: "A service or system overview.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\nowner:\nrepo:\ndepends_on: []\n---\n\n# {{title}}\n> What it does, one line.\n\n## Ownership\n\n## Interfaces\n\n## Dependencies\n\n## Related\n"},
		{ID: "entity", Label: "Entity", Description: "A person, organization, or important thing.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\ndescription:\n---\n\n# {{title}}\n> What this entity represents.\n\n## Fields\n\n| field | type | description |\n|-------|------|-------------|\n| id | uuid | Primary key |\n\n## Used by\n\n## Related entities\n"},
		{ID: "recipe", Label: "Recipe", Description: "A cooking recipe.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\ndescription:\ntags:\n  - recipe\nprep_time:\ncook_time:\ntotal_time:\nservings:\nsource_url:\n---\n\n# {{title}}\n\n## Summary\n\nBriefly describe the dish, when to make it, and what makes it work.\n\n## Details\n\n- **Prep time:**\n- **Cook time:**\n- **Total time:**\n- **Yield:**\n- **Category:**\n- **Cuisine:**\n\n## Ingredients\n\n- \n\n## Equipment\n\n- \n\n## Instructions\n\n1. \n\n## Tips\n\n- \n\n## Variations\n\n- \n\n## Storage\n\n- \n"},
		{ID: "gotcha", Label: "Gotcha", Description: "A pitfall and its solution.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\napplies_to: []\n---\n\n# {{title}}\n\n## What goes wrong\n\n## Why\n\n## What to do instead\n"},
		{ID: "convention", Label: "Convention", Description: "A shared standard or practice.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\napplies_to: []\n---\n\n# {{title}}\n\n## The convention\n\n## Rationale\n\n## Example\n\n## Exceptions\n"},
		{ID: "plan", Label: "Plan", Description: "A plan of work.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\nstatus: draft\nimplements: []\n---\n\n# {{title}}\n\n## Context / Goal\n\n## Approach\n\n## Areas affected\n\n## Risks\n\n## Verification\n"},
		{ID: "progress", Label: "Progress", Description: "A progress update.", Template: "---\ntype: {{type}}\ntitle: {{titleYaml}}\nplan:\nstatus: active\nupdated: {{date}}\n---\n\n# {{title}}\n\n## Done\n\n## In progress\n\n## Pending\n\n## Deferred / Blocked\n"},
	}
}

func upgradeGenericStarterTypes(root string) error {
	generic := "---\ntype: {{type}}\ntitle: {{titleYaml}}\n---\n\n# {{title}}\n\n## Notes\n\n"
	for _, definition := range starterWorkspaceNoteTypes() {
		path := filepath.Join(root, definition.ID+".yaml")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var existing NoteTypeDTO
		if yaml.Unmarshal(data, &existing) != nil || existing.Template != generic {
			continue
		}
		encoded, err := marshalNoteType(definition)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return err
		}
	}
	return nil
}
