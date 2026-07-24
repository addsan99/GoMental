// Package apphost is the front-door-agnostic host for GoMental. It constructs
// and owns the single application.Service for a process, owns the event fan-out
// Hub that lets many subscribers (a desktop WebView, N browser SSE clients)
// observe the same core events, and carries an Environment capability set that
// gates desktop-only behavior (e.g. native dialogs).
//
// Guardrail G2: all business logic stays in application.Service. The Wails
// bindings, the HTTP API, and the agent API are thin adapters over the same
// Host/Service instance. Guardrail G3: exactly one Service (and thus one Bleve
// index / SQLite graph) per workspace lives behind a Host.
package apphost

import (
	"context"

	"GoMental/internal/application"
	"GoMental/internal/workspace"
)

// Environment describes which host-level capabilities are available. Desktop
// (Wails) enables NativeDialogs; headless server mode does not.
type Environment struct {
	// NativeDialogs is true when OS file/directory pickers are available.
	NativeDialogs bool
}

// Desktop is the capability set for the Wails desktop app.
func Desktop() Environment { return Environment{NativeDialogs: true} }

// Headless is the capability set for server/agent (no OS dialogs).
func Headless() Environment { return Environment{NativeDialogs: false} }

// Config parameterizes Host construction. When StatePath is non-empty the
// provided RecentStore and StatePath are injected (used by tests and, later,
// server config); otherwise the process defaults are used.
type Config struct {
	Environment Environment
	RecentStore workspace.RecentWorkspaceStore
	StatePath   string
}

// Host owns the core Service, the event Hub, and the Environment for one process.
type Host struct {
	hub     *Hub
	service *application.Service
	env     Environment
}

// NewHost builds the event hub, constructs the core Service wired to publish
// through the hub, and returns a ready Host. The Service is transport-agnostic;
// nothing here depends on Wails.
func NewHost(cfg Config) (*Host, error) {
	hub := NewHub()
	var (
		service *application.Service
		err     error
	)
	if cfg.StatePath != "" {
		service = application.NewServiceWithStores(hub.Publish, cfg.RecentStore, cfg.StatePath)
	} else {
		service, err = application.NewService(hub.Publish)
		if err != nil {
			return nil, err
		}
	}
	return &Host{hub: hub, service: service, env: cfg.Environment}, nil
}

// Service returns the single core service owned by this host.
func (h *Host) Service() *application.Service { return h.service }

// Hub returns the event fan-out hub. Adapters subscribe here to receive core events.
func (h *Host) Hub() *Hub { return h.hub }

// Environment returns the host capability set.
func (h *Host) Environment() Environment { return h.env }

// OpenWorkspace is the headless open path: open a workspace by explicit path
// rather than via a native picker. Adapters (server bootstrap) call this.
func (h *Host) OpenWorkspace(ctx context.Context, root string) (application.WorkspaceDTO, error) {
	return h.service.OpenWorkspace(ctx, root)
}

// Close shuts down the service (stores, watchers) and detaches all subscribers.
func (h *Host) Close() error {
	err := h.service.Close()
	h.hub.Close()
	return err
}
