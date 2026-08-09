package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreListsSearchesReadsAndLoadsAssets(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "alpha.md"), `---
type: term
title: Alpha Systems
tags: [go, mobile]
favorite: true
---

# Alpha

Android bridge notes. See [[guides/beta]].
`)
	writeTestFile(t, filepath.Join(root, "guides", "beta.md"), `---
type: how-to
title: Beta Guide
---

# Beta

Read [Alpha](../alpha.md) and view ![diagram](images/diagram.txt).
`)
	writeTestFile(t, filepath.Join(root, "guides", "images", "diagram.txt"), "asset bytes")

	core := openTestCore(t, root)
	status, err := core.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(status, `"ready":true`) || !strings.Contains(status, `"noteCount":2`) {
		t.Fatalf("unexpected status: %s", status)
	}

	listed, err := core.ListNotes(`{"tags":["mobile"],"favoritesOnly":true}`)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	var list struct {
		Notes []noteDTO `json:"notes"`
	}
	if err := json.Unmarshal([]byte(listed), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Notes) != 1 || list.Notes[0].ID != "alpha" || list.Notes[0].Title != "Alpha Systems" {
		t.Fatalf("unexpected notes: %#v", list.Notes)
	}

	searched, err := core.Search(`{"text":"Android","limit":10}`)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(searched, `"id":"alpha"`) {
		t.Fatalf("search did not find alpha: %s", searched)
	}

	detail, err := core.ReadNote("alpha")
	if err != nil {
		t.Fatalf("ReadNote: %v", err)
	}
	if !strings.Contains(detail, `"resolvedId":"guides/beta"`) || !strings.Contains(detail, `"body":"`) {
		t.Fatalf("note detail missing resolved link or body: %s", detail)
	}
	if !strings.Contains(detail, `"key":"type","value":"term"`) || !strings.Contains(detail, `"id":"guides/beta","title":"Beta Guide"`) {
		t.Fatalf("note detail missing metadata or incoming links: %s", detail)
	}

	asset, err := core.LoadAsset("guides/beta", "images/diagram.txt")
	if err != nil {
		t.Fatalf("LoadAsset: %v", err)
	}
	if string(asset) != "asset bytes" {
		t.Fatalf("asset = %q", asset)
	}
	if _, err := core.LoadAsset("guides/beta", "../../outside.txt"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestOpenAllowsRepositoryToBeClonedLater(t *testing.T) {
	parent := t.TempDir()
	configJSON, err := json.Marshal(config{RepositoryPath: filepath.Join(parent, "repository"), DataPath: filepath.Join(parent, "data")})
	if err != nil {
		t.Fatal(err)
	}
	core, err := Open(string(configJSON))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = core.Close() })
	status, err := core.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, `"ready":false`) {
		t.Fatalf("unexpected status: %s", status)
	}
}

func TestOpenRejectsDataInsideRepository(t *testing.T) {
	root := t.TempDir()
	configJSON, err := json.Marshal(config{RepositoryPath: root, DataPath: filepath.Join(root, "data")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(string(configJSON)); err == nil {
		t.Fatal("expected overlapping paths to be rejected")
	}
}

func TestCoreRejectsAssetSymlinkOutsideRepository(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "alpha.md"), "---\ntype: general\n---\n\n# Alpha\n")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeTestFile(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(root, "secret.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	core := openTestCore(t, root)
	if _, err := core.LoadAsset("alpha", "secret.txt"); err == nil {
		t.Fatal("expected external symlink to be rejected")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	root := t.TempDir()
	core := openTestCore(t, root)
	if err := core.Close(); err != nil {
		t.Fatal(err)
	}
	if err := core.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Status(); err == nil {
		t.Fatal("expected closed core status to fail")
	}
}

func openTestCore(t *testing.T, root string) *Core {
	t.Helper()
	configJSON, err := json.Marshal(config{RepositoryPath: root, DataPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	core, err := Open(string(configJSON))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return core
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
