package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkspaceURLPathDiscoveryUsesChildDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"workspaceA", "workspaceB", ".workspace"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-workspace.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	children, err := discoverWorkspaceChildren(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := make([]string, 0, len(children))
	for _, child := range children {
		got = append(got, filepath.Base(child))
	}
	want := []string{"workspaceA", "workspaceB"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("children: got %#v want %#v", got, want)
	}
}

func TestUniqueWorkspaceSlugsAddsSuffixesForDuplicateNames(t *testing.T) {
	roots := []string{
		filepath.Join("one", "workspaceA"),
		filepath.Join("two", "workspaceA"),
		filepath.Join("three", "workspaceA"),
		filepath.Join("four", "workspaceB"),
	}
	got := uniqueWorkspaceSlugs(roots)
	want := []string{"workspaceA", "workspaceA(1)", "workspaceA(2)", "workspaceB"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slugs: got %#v want %#v", got, want)
	}
}

func TestWorkspaceURLPathDiscoveryFindsNestedDuplicateWorkspaceNames(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		filepath.Join("team-one", "workspaceA"),
		filepath.Join("team-two", "workspaceA"),
	} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("---\ntype: concept\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	children, err := discoverWorkspaceChildren(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := uniqueWorkspaceSlugs(children)
	want := []string{"workspaceA", "workspaceA(1)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slugs: got %#v want %#v", got, want)
	}
}
