package apphost

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"GoMental/internal/application"
	"GoMental/internal/workspace"
)

func testHost(t *testing.T) *Host {
	t.Helper()
	store := workspace.NewRecentWorkspaceStore(filepath.Join(t.TempDir(), "recent.json"), 10)
	host, err := NewHost(Config{
		Environment: Headless(),
		RecentStore: store,
		StatePath:   filepath.Join(t.TempDir(), "ui-state.json"),
	})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
}

func writeNote(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitFor drains sub until it observes an event named want, or fails after a timeout.
func waitFor(t *testing.T, sub *Subscription, want string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatalf("subscription closed before observing %q", want)
			}
			if ev.Name == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %q", want)
		}
	}
}

// TestHostFanOutTwoSubscribers is the Phase 16 acceptance criterion: construct a
// Host, open a fixed workspace path, subscribe two listeners, save a note, and
// observe both listeners receive note:updated.
func TestHostFanOutTwoSubscribers(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha\n")

	host := testHost(t)
	ctx := context.Background()
	if _, err := host.OpenWorkspace(ctx, root); err != nil {
		t.Fatalf("open workspace: %v", err)
	}

	subA := host.Hub().Subscribe(0)
	defer subA.Close()
	subB := host.Hub().Subscribe(0)
	defer subB.Close()
	if got := host.Hub().SubscriberCount(); got != 2 {
		t.Fatalf("expected 2 subscribers, got %d", got)
	}

	req := application.SaveNoteRequest{ID: "alpha", Content: "---\ntype: concept\ntitle: Alpha\ntags: [go]\n---\n\n# Alpha updated\n"}
	if _, err := host.Service().SaveNote(ctx, req); err != nil {
		t.Fatalf("save note: %v", err)
	}

	waitFor(t, subA, "note:updated")
	waitFor(t, subB, "note:updated")
}

// TestHostEnvironmentCapabilities documents the desktop/headless capability split.
func TestHostEnvironmentCapabilities(t *testing.T) {
	if !Desktop().NativeDialogs {
		t.Fatal("desktop environment must expose native dialogs")
	}
	if Headless().NativeDialogs {
		t.Fatal("headless environment must not expose native dialogs")
	}
}

// TestHubDropsOnSlowConsumer verifies a full-buffer subscriber does not block peers.
func TestHubDropsOnSlowConsumer(t *testing.T) {
	hub := NewHub()
	slow := hub.Subscribe(1) // never drained
	fast := hub.Subscribe(8)
	for i := 0; i < 20; i++ {
		hub.Publish("tick", i)
	}
	// fast should still be receiving; slow simply dropped the overflow.
	select {
	case ev, ok := <-fast.Events():
		if !ok || ev.Name != "tick" {
			t.Fatalf("fast subscriber did not receive events: %v ok=%v", ev, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publisher stalled on slow consumer")
	}
	_ = slow
	hub.Close()
}
