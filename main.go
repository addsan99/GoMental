package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"GoMental/internal/agentapi/mcp"
	"GoMental/internal/apphost"
	"GoMental/internal/auth"
	"GoMental/internal/gitsync"
	"GoMental/internal/httpapi"
	"GoMental/internal/serverconfig"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	logStartup("main() entry")
	// Subcommand dispatch. With no subcommand (the default), run the Wails
	// desktop app exactly as before (Guardrail G1). Server/agent subcommands
	// run headless.
	if len(os.Args) > 1 {
		// This is a CLI invocation (a subcommand or a flag), so make sure our
		// output is actually visible: on Windows the GUI-subsystem binary starts
		// with no console, so attach to the launching terminal's. No-op elsewhere
		// and on GUI double-clicks.
		attachParentConsole()
		switch os.Args[1] {
		case "serve":
			if err := runServe(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "serve error:", err)
				os.Exit(1)
			}
			return
		case "viewer":
			if err := runViewer(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "viewer error:", err)
				os.Exit(1)
			}
			return
		case "mcp":
			if err := runMCP(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "mcp error:", err)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help", "-help", "-?", "/?", "/h", "/help":
			printUsage(os.Stdout)
			return
		default:
			// A token that looks like a flag or command (starts with "-") is a CLI
			// invocation, not a double-click — printing usage beats silently
			// launching the GUI (which confuses agents probing for options).
			if len(os.Args[1]) > 0 && os.Args[1][0] == '-' {
				fmt.Fprintf(os.Stderr, "GoMental: unknown option %q\n\n", os.Args[1])
				printUsage(os.Stderr)
				os.Exit(2)
			}
			// Any other stray token: fall through to the desktop app to preserve
			// the current behavior of ignoring arguments (Guardrail G1).
		}
	}
	runDesktop()
}

// printUsage writes the command-line reference. It documents every subcommand
// and, importantly, makes clear that `mcp` is a stdio JSON-RPC server driven by
// an MCP client — not an interactive command and not the desktop GUI (which is
// the no-argument default).
func printUsage(w io.Writer) {
	io.WriteString(w, `GoMental — local-first OKF notes wiki for humans and LLMs.

USAGE:
    GoMental [command] [flags]

COMMANDS:
    (no command)   Launch the desktop application (default; opens the GUI). Any
                   stray non-flag argument is ignored and the GUI still opens.
    mcp            Run a stdio MCP server so a *local* coding agent can read/write
                   a workspace on this machine. Speaks JSON-RPC 2.0 over
                   stdin/stdout — it is NOT interactive and NOT the GUI. Launch it
                   from an MCP client, not by hand; run standalone it opens the
                   workspace, prints a readiness line to stderr, then exits at
                   stdin EOF. For a central/remote server, use serve's /mcp
                   endpoint instead (see below).
    serve          Run the HTTP API + web UI server (long-running daemon). Also
                   hosts the MCP tool surface over HTTP at POST /mcp so remote
                   coding agents reach the same workspace as the browser UI.
    viewer         Launch the desktop GUI as a read-only viewer over a fixed
                   workspace, optionally tracking a git remote as the source of
                   truth (clones on first open, then fetch+reset on pull). Use
                   this for the offline/solo curator who wants to browse a
                   git-managed wiki without running the 'serve' daemon.
    help           Show this help. Aliases: -h, --help, -help, -?, /?, /h, /help.

MCP  (gomental mcp --flags):
    --workspace <path>   OKF workspace to serve (or set GOMENTAL_WORKSPACE).

    Wire it into an MCP client's config, e.g.:
        {
          "mcpServers": {
            "gomental": {
              "command": "/path/to/GoMental",
              "args": ["mcp", "--workspace", "/path/to/wiki"]
            }
          }
        }
    Tools exposed: search_wiki, read_note, list_notes, create_note, edit_note,
    upload_asset, backlinks, neighborhood, expand_context, explain_link.

    Remote agents (central server): point an HTTP MCP client at a running
    'serve' instance instead of spawning this subcommand:
        POST https://<host>/mcp   (Streamable-HTTP JSON-RPC; Bearer API key)
    The same tools are served over that endpoint, backed by the one shared
    workspace. Read tools need a viewer key; the write tools (create_note,
    edit_note, upload_asset) need editor.

    Smoke-test by hand (pipe one request in and see the response):
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
          | GoMental mcp --workspace /path/to/wiki

SERVE  (gomental serve --flags):
    --workspace <path>   OKF workspace to serve (or GOMENTAL_WORKSPACE).
    --addr <addr>        Listen address (default :8080 / $GOMENTAL_ADDR).
    --auth <mode>        Auth mode: trustall (default; LAN / behind a proxy).
    --tls-cert <file>    TLS certificate (enables HTTPS with --tls-key).
    --tls-key <file>     TLS private key (enables HTTPS with --tls-cert).
    --cors-origin <list> Comma-separated CORS allow-list of exact origins.
    --request-rate <n>   Per-actor request rate/sec (default 50).
    --write-rate <n>     Per-actor write rate/sec (default 10).
    --config <file>      JSON config file (lowest precedence).

  Git-viewer mode (the notes repo is managed in git; GoMental is a read replica):
    --git-remote <url>   Track this git remote as the source of truth. Enables
                         git-viewer mode: --workspace becomes a working copy that
                         is cloned if absent and advanced by fetch+reset. Turns on
                         --read-only unless you set it explicitly.
    --git-ref <ref>      Branch/tag to track (default main).
    --git-poll <dur>     Poll interval for git fetch, e.g. 2m (default off; use a
                         webhook to POST /api/git/sync instead).
    --git-webhook-secret <s>  Shared secret so a git host webhook can trigger a
                         pull via POST /api/git/sync without an API key
                         (X-GoMental-Token header or ?token=).
    --read-only <bool>   Reject content writes (create/edit/delete/import + MCP
                         write tools). Default: true when --git-remote is set.

    Recommendation: for a curated, multi-viewer wiki, run ONE server against ONE
    working copy (a read replica with a shared graph) rather than cloning and
    indexing on every client — the index is built once and every viewer agrees on
    the same commit. See docs/GIT_SYNC.md.

VIEWER  (gomental viewer --flags):
    Launches the desktop GUI over a pinned workspace. Same workspace/git/read-only
    flags as serve; no network flags (it is a local GUI, not a daemon):
    --workspace <path>   OKF workspace / git working copy to open (required unless
                         --git-remote is set, in which case it is the clone target).
    --git-remote <url>   Track this git remote as the source of truth (clones on
                         first open, then fetch+reset on pull). Turns on read-only
                         unless you set it explicitly. Use the git status chip in
                         the header to pull the latest commit.
    --git-ref <ref>      Branch/tag to track (default main).
    --git-poll <dur>     Auto-pull interval, e.g. 5m (default off; pull manually
                         via the header chip). No webhook — this is a local GUI.
    --read-only <bool>   Reject content writes. Default: true when --git-remote set.
    --config <file>      JSON config file (lowest precedence).

ENVIRONMENT:
    GOMENTAL_WORKSPACE   Default workspace path for mcp/serve/viewer.
    GOMENTAL_ADDR        Default listen address for serve.
    GOMENTAL_GIT_REMOTE  Default git remote (enables git mode for serve/viewer).
    GOMENTAL_GIT_REF     Default tracked ref. GOMENTAL_GIT_POLL: poll interval.
    GOMENTAL_READ_ONLY   Reject content writes (serve/viewer); as --read-only.
    GOMENTAL_GIT_WEBHOOK_SECRET: webhook secret (serve only); as --git-webhook-secret.

EXAMPLES:
    GoMental                                  # launch the desktop app
    GoMental --help                           # print this help
    GoMental serve --workspace C:\wiki        # serve the web UI + /mcp on :8080
    GoMental serve --workspace C:\wiki --addr :9000 --tls-cert cert.pem --tls-key key.pem
    GoMental serve --workspace /srv/wiki --git-remote https://github.com/org/wiki.git --git-ref main --git-poll 2m
    GoMental viewer --workspace C:\wiki-copy --git-remote https://github.com/org/wiki.git --git-poll 5m
    GoMental mcp --workspace C:\wiki          # stdio MCP server for a local agent

NOTES:
    Flags accept either "--flag value" or "--flag=value".
    On Windows this is a GUI-subsystem binary, so CLI output is written to the
    launching terminal's console and may appear just below the returned prompt.
`)
}

// runDesktop launches the Wails desktop application. This is the default path
// and is unchanged from the pre-server main().
func runDesktop() {
	runWailsApp(NewApp())
}

// runViewer launches the desktop GUI in viewer (reader) mode: the same Wails app
// bound to a workspace pinned by --workspace, optionally tracking a git remote as
// the source of truth (--git-remote) and rejecting content writes (--read-only,
// default on in git mode). It reuses serverconfig for flag/env/file resolution
// and validation, so `viewer` accepts the same workspace/git/read-only flags as
// `serve`; the server-only network flags (addr, tls, cors, rates, webhook) do
// not apply to a local GUI and are omitted.
func runViewer(args []string) error {
	fset := flag.NewFlagSet("viewer", flag.ContinueOnError)
	fset.Usage = func() { printUsage(os.Stderr) }
	configFile := fset.String("config", "", "path to a JSON config file (lowest precedence)")
	workspace := fset.String("workspace", "", "path to the OKF workspace / git working copy (or $GOMENTAL_WORKSPACE)")
	gitRemote := fset.String("git-remote", "", "git remote URL to track as the source of truth (enables git-viewer mode)")
	gitRef := fset.String("git-ref", "", "git branch/tag to track (default main)")
	gitPoll := fset.String("git-poll", "", "poll interval for git fetch, e.g. 2m (default off)")
	readOnly := fset.String("read-only", "", "reject content writes: true/false (default true when --git-remote is set)")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // usage already printed by fset.Usage
		}
		return err
	}

	cfg, err := serverconfig.Load(serverconfig.Options{
		ConfigFile:    *configFile,
		WorkspaceRoot: *workspace,
		GitRemote:     *gitRemote,
		GitRef:        *gitRef,
		GitPoll:       *gitPoll,
		ReadOnly:      *readOnly,
	})
	if err != nil {
		return err
	}

	logger := log.New(os.Stderr, "[gomental] ", log.LstdFlags|log.LUTC)
	if cfg.GitEnabled() {
		logger.Printf("viewer (git) mode: tracking %s@%s in %s (read-only=%v)", cfg.GitRemote, cfg.GitRef, cfg.WorkspaceRoot, cfg.ReadOnly)
	} else {
		logger.Printf("viewer mode: %s (read-only=%v)", cfg.WorkspaceRoot, cfg.ReadOnly)
	}

	runWailsApp(NewViewerApp(cfg, logger))
	return nil
}

// runWailsApp paints the native splash and hands off to WebView2, bound to the
// given app. Shared by the desktop (no-arg) and viewer entry points.
func runWailsApp(app *App) {
	// Paint the native splash first thing — WebView2 can take several seconds to
	// initialise, during which Wails shows no window at all on Windows. No-op on
	// other platforms. It is dismissed in App.domReady when the real UI paints.
	showSplash()

	logStartup("runWailsApp: before wails.Run (handing off to WebView2)")
	err := wails.Run(&options.App{
		Title:     "GoMental",
		Width:     1280,
		Height:    820,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 245, G: 244, B: 239, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

// runServe parses server flags, builds a headless apphost.Host, opens the
// configured workspace, and serves the HTTP API + SPA until interrupted.
func runServe(args []string) error {
	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	fset.Usage = func() { printUsage(os.Stderr) }
	configFile := fset.String("config", "", "path to a JSON config file (lowest precedence)")
	addr := fset.String("addr", "", "listen address (default :8080 / $GOMENTAL_ADDR)")
	workspace := fset.String("workspace", "", "path to the OKF workspace to serve (or $GOMENTAL_WORKSPACE)")
	authMode := fset.String("auth", "", "auth mode: trustall (default; LAN-trusted / behind a reverse proxy)")
	tlsCert := fset.String("tls-cert", "", "TLS certificate file (enables HTTPS with --tls-key)")
	tlsKey := fset.String("tls-key", "", "TLS private key file (enables HTTPS with --tls-cert)")
	corsOrigins := fset.String("cors-origin", "", "comma-separated CORS allow-list of exact origins")
	requestRate := fset.String("request-rate", "", "per-actor request rate/sec (default 50)")
	writeRate := fset.String("write-rate", "", "per-actor write rate/sec (default 10)")
	gitRemote := fset.String("git-remote", "", "git remote URL to track as the source of truth (enables git-viewer mode)")
	gitRef := fset.String("git-ref", "", "git branch/tag to track (default main)")
	gitPoll := fset.String("git-poll", "", "poll interval for git fetch, e.g. 2m (default off; webhook-driven)")
	gitWebhookSecret := fset.String("git-webhook-secret", "", "shared secret authenticating keyless POST /api/git/sync (webhook)")
	readOnly := fset.String("read-only", "", "reject content writes: true/false (default true when --git-remote is set)")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // usage already printed by fset.Usage
		}
		return err
	}

	cfg, err := serverconfig.Load(serverconfig.Options{
		ConfigFile:       *configFile,
		Addr:             *addr,
		WorkspaceRoot:    *workspace,
		AuthMode:         *authMode,
		TLSCertFile:      *tlsCert,
		TLSKeyFile:       *tlsKey,
		CORSOrigins:      *corsOrigins,
		RequestRate:      *requestRate,
		WriteRate:        *writeRate,
		GitRemote:        *gitRemote,
		GitRef:           *gitRef,
		GitPoll:          *gitPoll,
		GitWebhookSecret: *gitWebhookSecret,
		ReadOnly:         *readOnly,
	})
	if err != nil {
		return err
	}

	logger := log.New(os.Stderr, "[gomental] ", log.LstdFlags|log.LUTC)

	host, err := apphost.NewHost(apphost.Config{Environment: apphost.Headless()})
	if err != nil {
		return fmt.Errorf("create host: %w", err)
	}
	defer func() { _ = host.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Git-viewer mode: the workspace is a working copy of an external, curated
	// repo (the source of truth). Materialize the clone FIRST — before anything
	// (API-key store, audit log, workspace open) writes GoMental metadata into the
	// workspace dir, since `git clone` refuses a non-empty target. The workspace
	// watcher started later by OpenWorkspace then reconciles subsequent pulls
	// incrementally; this manager only advances the checkout.
	var gitManager *gitsync.Manager
	if cfg.GitEnabled() {
		gitManager, err = gitsync.New(gitsync.Config{
			Remote: cfg.GitRemote,
			Ref:    cfg.GitRef,
			Dir:    cfg.WorkspaceRoot,
			Notify: host.Hub().Publish,
		})
		if err != nil {
			return fmt.Errorf("configure git sync: %w", err)
		}
		logger.Printf("git-viewer mode: tracking %s@%s in %s (read-only=%v)", cfg.GitRemote, cfg.GitRef, cfg.WorkspaceRoot, cfg.ReadOnly)
		if err := gitManager.Ensure(ctx); err != nil {
			return fmt.Errorf("prepare git working copy: %w", err)
		}
		if _, err := gitManager.Sync(ctx); err != nil {
			// Non-fatal: serve the current checkout even if the initial fetch
			// failed (e.g. transient network). Status surfaces the error.
			logger.Printf("initial git sync failed (serving current checkout): %v", err)
		}
	}

	// Trust-all posture (LAN) with optional API-key attribution: a request that
	// presents a valid Bearer key is attributed to that key's actor/role (and
	// audited); a keyless request falls back to the local admin actor; an
	// invalid/revoked key is rejected. Set RequireKey on the authenticator to
	// enforce identity fully — the role gates and audit trail are already wired.
	// These open under the workspace's .workspace/ dir, so they run only after the
	// git clone (if any) has populated the workspace.
	keyStore, err := auth.OpenAPIKeyStore(auth.DefaultAPIKeyPath(cfg.WorkspaceRoot))
	if err != nil {
		return fmt.Errorf("open api key store: %w", err)
	}
	authenticator := auth.BearerAuthenticator{Store: keyStore, Fallback: auth.LocalActor, RequireKey: false}
	auditLog, err := auth.OpenAuditLog(auth.DefaultAuditPath(cfg.WorkspaceRoot))
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	if !authenticator.Enforced() {
		logger.Printf("auth mode %q: identity NOT enforced — run on a trusted network or behind an authenticating reverse proxy", cfg.AuthMode)
	}

	if _, err := host.OpenWorkspace(ctx, cfg.WorkspaceRoot); err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	if gitManager != nil && cfg.GitPollInterval > 0 {
		logger.Printf("git poll enabled: fetching %s every %s", cfg.GitRef, cfg.GitPollInterval)
		go gitManager.RunPoll(ctx, cfg.GitPollInterval)
	}

	opts := httpapi.Options{
		Host:             host,
		Config:           cfg,
		StaticFS:         spaAssets(),
		Logger:           logger,
		Auth:             authenticator,
		Audit:            auditLog,
		KeyStore:         keyStore,
		RequestRate:      cfg.RequestRate,
		WriteRate:        cfg.WriteRate,
		AllowedOrigins:   cfg.AllowedOrigins,
		TLSEnabled:       cfg.TLSEnabled(),
		ReadOnly:         cfg.ReadOnly,
		GitWebhookSecret: cfg.GitWebhookSecret,
	}
	if gitManager != nil {
		opts.GitManager = gitManager
	}
	server := httpapi.NewServer(opts)
	return server.ListenAndServe(ctx)
}

// runMCP runs a stdio MCP server for local coding agents, reusing an in-process
// Service over the given workspace. Protocol traffic is on stdin/stdout; logs go
// to stderr so they never corrupt the JSON-RPC stream.
func runMCP(args []string) error {
	fset := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fset.Usage = func() { printUsage(os.Stderr) }
	workspace := fset.String("workspace", "", "path to the OKF workspace to serve (or $GOMENTAL_WORKSPACE)")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // usage already printed by fset.Usage
		}
		return err
	}
	root := *workspace
	if root == "" {
		root = os.Getenv(serverconfig.EnvWorkspace)
	}
	if root == "" {
		return fmt.Errorf("--workspace is required (or set %s)", serverconfig.EnvWorkspace)
	}

	host, err := apphost.NewHost(apphost.Config{Environment: apphost.Headless()})
	if err != nil {
		return fmt.Errorf("create host: %w", err)
	}
	defer func() { _ = host.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := host.OpenWorkspace(ctx, root); err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[gomental] mcp stdio server ready (workspace %s)\n", root)
	server := mcp.NewServer(host.Service())
	return server.Run(ctx, os.Stdin, os.Stdout)
}

// spaAssets returns the embedded SPA bundle rooted at the dist directory, or nil
// if the bundle is absent (the server then serves a placeholder shell).
func spaAssets() fs.FS {
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return nil
	}
	// Confirm index.html exists; otherwise treat as absent.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
