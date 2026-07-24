package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"GoMental/internal/apphost"
	"GoMental/internal/application"
	"GoMental/internal/gitsync"
	"GoMental/internal/serverconfig"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// errReadOnly is returned by the content-mutating bindings when the desktop app
// runs in read-only viewer mode (its git working copy is the source of truth).
// It mirrors the server's workspace.read_only 403; the frontend also disables
// authoring UI when Info().readOnly is set, so this is defense in depth.
var errReadOnly = errors.New("workspace is read-only — content is managed in git")

// App is the Wails desktop adapter. It is a thin front door over an
// apphost.Host: all business logic lives in the core application.Service, and
// the WebView is registered as one subscriber of the host event hub. This is
// the only place wailsruntime is referenced (Guardrail G2).
type App struct {
	ctx     context.Context
	mu      sync.Mutex
	host    *apphost.Host
	sub     *apphost.Subscription
	subOnce sync.Once

	// viewer is non-nil only when launched via the `viewer` subcommand. It pins
	// the workspace to a configured root, optionally tracks a git remote as the
	// source of truth, and (in git mode) makes the workspace read-only.
	viewer *viewerRuntime
}

type AppInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Phase       string `json:"phase"`
	// The fields below are populated only in viewer mode. They mirror the shape
	// the HTTP /api/info handler emits so the SPA's git-viewer UX (mode gate,
	// read-only banner, git status chip) works identically over Wails bindings.
	Mode      string        `json:"mode,omitempty"`
	Workspace string        `json:"workspace,omitempty"`
	ReadOnly  bool          `json:"readOnly,omitempty"`
	Git       *appGitStatus `json:"git,omitempty"`
}

// appGitStatus is the JSON shape the SPA's GitStatusInfo type consumes. It is
// the desktop counterpart of httpapi.gitStatusJSON.
type appGitStatus struct {
	Remote     string  `json:"remote"`
	Ref        string  `json:"ref"`
	Commit     string  `json:"commit"`
	LastSyncAt *string `json:"lastSyncAt"`
	LastError  string  `json:"lastError"`
	Syncing    bool    `json:"syncing"`
}

// GitSyncResult is returned by the GitSync binding — the same fields the HTTP
// POST /api/git/sync handler reports.
type GitSyncResult struct {
	OK        bool   `json:"ok"`
	OldCommit string `json:"oldCommit"`
	NewCommit string `json:"newCommit"`
	Changed   int    `json:"changed"`
	Deleted   int    `json:"deleted"`
}

// viewerRuntime holds the pinned workspace + optional git tracking state for the
// `viewer` subcommand. The git manager only advances the checkout; the host's
// workspace watcher reconciles the resulting file changes (same design as the
// server's git-viewer mode).
type viewerRuntime struct {
	root     string
	remote   string // "" → plain read-only viewer (no git tracking)
	ref      string
	poll     time.Duration
	readOnly bool
	logger   *log.Logger

	mgr      *gitsync.Manager
	bootOnce sync.Once
	bootErr  error
	pollOnce sync.Once
}

func NewApp() *App {
	return &App{}
}

// NewViewerApp builds the desktop app in viewer mode from a resolved server
// config. Git materialization is deferred to the first OpenWorkspace so startup
// (and the WebView first paint) is never blocked on a clone.
func NewViewerApp(cfg serverconfig.Config, logger *log.Logger) *App {
	return &App{viewer: &viewerRuntime{
		root:     cfg.WorkspaceRoot,
		remote:   cfg.GitRemote,
		ref:      cfg.GitRef,
		poll:     cfg.GitPollInterval,
		readOnly: cfg.ReadOnly,
		logger:   logger,
	}}
}

// ensureBoot clones/adopts the working copy and runs the initial fetch exactly
// once. Ensure failures are fatal (returned to the caller); an initial Sync
// failure is non-fatal — we serve the current checkout and surface the error via
// Status. No-op when git tracking is disabled.
func (v *viewerRuntime) ensureBoot(ctx context.Context) error {
	v.bootOnce.Do(func() {
		if v.mgr == nil {
			return
		}
		if err := v.mgr.Ensure(ctx); err != nil {
			v.bootErr = fmt.Errorf("prepare git working copy: %w", err)
			return
		}
		if _, err := v.mgr.Sync(ctx); err != nil {
			v.logger.Printf("initial git sync failed (serving current checkout): %v", err)
		}
	})
	return v.bootErr
}

// startPoll launches the background fetch loop once, after the workspace (and
// therefore its watcher) is open. No-op when polling is disabled or git is off.
func (v *viewerRuntime) startPoll(ctx context.Context) {
	if v.mgr == nil || v.poll <= 0 {
		return
	}
	v.pollOnce.Do(func() {
		v.logger.Printf("git poll enabled: fetching %s every %s", v.ref, v.poll)
		go v.mgr.RunPoll(ctx, v.poll)
	})
}

func (a *App) startup(ctx context.Context) {
	logStartup("OnStartup (WebView2 environment created)")
	a.ctx = ctx
	// Register the WebView emitter as one subscriber of the host event hub.
	a.subOnce.Do(func() {
		host := a.mustHost()
		sub := host.Hub().Subscribe(0)
		a.sub = sub
		go func() {
			for ev := range sub.Events() {
				if a.ctx != nil {
					wailsruntime.EventsEmit(a.ctx, ev.Name, ev.Payload)
				}
			}
		}()

		// Viewer + git mode: build the manager now (cheap; no I/O until Ensure)
		// so Info() can report the git chip before the first pull. Notify routes
		// git:synced/git:sync-error through the same hub the WebView subscribes to.
		if a.viewer != nil && a.viewer.remote != "" {
			mgr, err := gitsync.New(gitsync.Config{
				Remote: a.viewer.remote,
				Ref:    a.viewer.ref,
				Dir:    a.viewer.root,
				Notify: host.Hub().Publish,
			})
			if err != nil {
				a.viewer.bootErr = fmt.Errorf("configure git sync: %w", err)
			} else {
				a.viewer.mgr = mgr
			}
		}
	})
}

// domReady fires when the WebView has finished loading the frontend document —
// i.e. the first moment anything is actually on screen. The gap between
// OnStartup and here is WebView2 loading/rendering the embedded SPA shell.
func (a *App) domReady(ctx context.Context) {
	logStartup("OnDomReady (frontend first paint)")
	// The real UI is now on screen — dismiss the native pre-render splash.
	closeSplash()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	host := a.host
	sub := a.sub
	a.host = nil
	a.sub = nil
	a.mu.Unlock()
	if sub != nil {
		sub.Close()
	}
	if host != nil {
		_ = host.Close()
	}
}

func (a *App) Info() AppInfo {
	info := AppInfo{Name: "GoMental", Description: "Local-first OKF notes and knowledge graph", Phase: "Phase 16 server host"}
	if a.viewer != nil {
		info.Mode = "viewer"
		info.Workspace = a.viewer.root
		info.ReadOnly = a.viewer.readOnly
		if a.viewer.mgr != nil {
			info.Git = gitStatusToJSON(a.viewer.mgr.Status())
		} else if a.viewer.remote != "" {
			// Manager not yet built (pre-startup) — advertise the target so the
			// chip renders "ref @ (not yet cloned)".
			info.Git = &appGitStatus{Remote: a.viewer.remote, Ref: a.viewer.ref}
		}
	}
	return info
}

// gitStatusToJSON is the desktop analogue of httpapi.gitStatusJSON.
func gitStatusToJSON(st gitsync.Status) *appGitStatus {
	var lastSyncAt *string
	if st.LastSyncAt != nil {
		s := st.LastSyncAt.UTC().Format(time.RFC3339)
		lastSyncAt = &s
	}
	return &appGitStatus{
		Remote:     st.Remote,
		Ref:        st.Ref,
		Commit:     st.Commit,
		LastSyncAt: lastSyncAt,
		LastError:  st.LastError,
		Syncing:    st.Syncing,
	}
}

// GitSync triggers a fetch+reset of the working copy (viewer git mode only). The
// workspace watcher reconciles the resulting content changes and emits
// note/graph events; the manager emits git:synced, which the SPA toasts.
func (a *App) GitSync() (GitSyncResult, error) {
	if a.viewer == nil || a.viewer.mgr == nil {
		return GitSyncResult{}, errors.New("not in git-viewer mode")
	}
	res, err := a.viewer.mgr.Sync(a.context())
	if err != nil {
		return GitSyncResult{}, err
	}
	return GitSyncResult{
		OK:        true,
		OldCommit: res.OldCommit,
		NewCommit: res.NewCommit,
		Changed:   len(res.Changed),
		Deleted:   len(res.Deleted),
	}, nil
}

func (a *App) SelectWorkspaceDirectory() (string, error) {
	if !a.mustHost().Environment().NativeDialogs {
		return "", errors.New("native directory picker is not available in server mode")
	}
	return wailsruntime.OpenDirectoryDialog(a.context(), wailsruntime.OpenDialogOptions{Title: "Open OKF Workspace"})
}
func (a *App) OpenWorkspace(root string) (application.WorkspaceDTO, error) {
	if a.viewer != nil {
		// Materialize the clone before opening (git clone refuses a non-empty
		// target, and the workspace open writes .workspace/ metadata). Pin to the
		// configured root so the "Open" button can't wander off the tracked copy.
		if err := a.viewer.ensureBoot(a.context()); err != nil {
			return application.WorkspaceDTO{}, err
		}
		root = a.viewer.root
	}
	dto, err := a.service().OpenWorkspace(a.context(), root)
	if err == nil && a.viewer != nil {
		// Start polling only after the watcher is live, so pulled changes are
		// reconciled.
		a.viewer.startPoll(a.context())
	}
	return dto, err
}

func (a *App) ListNotes() ([]application.NoteSummaryDTO, error) {
	return a.service().ListNotes(a.context())
}

func (a *App) ListNotesPage(req application.ListNotesQueryDTO) (application.NotesPageDTO, error) {
	return a.service().ListNotesPage(a.context(), req)
}

func (a *App) ReadNote(id string) (application.NoteDTO, error) {
	return a.service().ReadNote(a.context(), id)
}

func (a *App) SaveNote(req application.SaveNoteRequest) (application.NoteDTO, error) {
	if a.writesBlocked() {
		return application.NoteDTO{}, errReadOnly
	}
	return a.service().SaveNote(a.context(), req)
}

func (a *App) ImportURL(req application.ImportURLRequest) (application.NoteDTO, error) {
	if a.writesBlocked() {
		return application.NoteDTO{}, errReadOnly
	}
	return a.service().ImportURL(a.context(), req)
}

func (a *App) SaveNoteAsset(req application.SaveNoteAssetRequest) (application.SaveNoteAssetResponse, error) {
	if a.writesBlocked() {
		return application.SaveNoteAssetResponse{}, errReadOnly
	}
	return a.service().SaveNoteAsset(a.context(), req)
}

func (a *App) LoadNoteAssetDataURL(req application.NoteAssetRequest) (string, error) {
	return a.service().LoadNoteAssetDataURL(a.context(), req)
}

func (a *App) DeleteNote(id string) error {
	if a.writesBlocked() {
		return errReadOnly
	}
	return a.service().DeleteNote(a.context(), id)
}

func (a *App) MoveNote(req application.MoveNoteRequest) (application.NoteDTO, error) {
	if a.writesBlocked() {
		return application.NoteDTO{}, errReadOnly
	}
	return a.service().MoveNote(a.context(), req)
}

// writesBlocked reports whether content-mutating bindings should be rejected —
// true in read-only viewer mode.
func (a *App) writesBlocked() bool {
	return a.viewer != nil && a.viewer.readOnly
}

func (a *App) Search(req application.SearchQueryDTO) ([]application.SearchResultDTO, error) {
	return a.service().Search(a.context(), req)
}

func (a *App) FullGraph(filter application.GraphFilterDTO) (application.GraphDTO, error) {
	return a.service().FullGraph(a.context(), filter)
}

func (a *App) Neighborhood(id string, depth int) (application.GraphDTO, error) {
	return a.service().Neighborhood(a.context(), id, depth)
}

func (a *App) GraphQuery(query application.GraphQueryDTO) (application.GraphDTO, error) {
	return a.service().GraphQuery(a.context(), query)
}

func (a *App) Backlinks(id string) ([]application.NoteLinkDTO, error) {
	return a.service().Backlinks(a.context(), id)
}

func (a *App) LoadGraphLayout() (application.LayoutSnapshotDTO, error) {
	return a.service().LoadGraphLayout(a.context())
}

func (a *App) SaveGraphLayout(snapshot application.LayoutSnapshotDTO) error {
	return a.service().SaveGraphLayout(a.context(), snapshot)
}

func (a *App) Rebuild() (application.RebuildResultDTO, error) {
	return a.service().Rebuild(a.context())
}

func (a *App) RecentWorkspaces() ([]application.RecentWorkspaceDTO, error) {
	return a.service().RecentWorkspaces(a.context())
}

func (a *App) LoadUIState() (application.UIState, error) {
	return a.service().LoadUIState(a.context())
}

func (a *App) SaveUIState(state application.UIState) error {
	return a.service().SaveUIState(a.context(), state)
}

// mustHost lazily constructs the desktop host (native dialogs enabled) on first use.
func (a *App) mustHost() *apphost.Host {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.host != nil {
		return a.host
	}
	host, err := apphost.NewHost(apphost.Config{Environment: apphost.Desktop()})
	if err != nil {
		panic(err)
	}
	a.host = host
	return host
}

func (a *App) service() *application.Service {
	return a.mustHost().Service()
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
