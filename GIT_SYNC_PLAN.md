# GIT_SYNC_PLAN — Connecting GoMental to a git-managed notes repository

Status: **A–D complete & verified green** (2026-07-20; `go test ./...` + `npm run typecheck`
pass; smoke-tested end-to-end with `serve --git-remote` against a local bare repo). Phase E
(out-of-tree index) deferred. Companion to `SERVER_COLLAB_PLAN.md` and
`WIKI_LLM_SCENARIOS.md`. Guardrails carry over: `go test ./...` + `cd frontend && npm run
typecheck` green at each phase; the Wails desktop default path (`gomental` with no
subcommand) stays untouched (G1); business logic stays in `application.Service` and every
front door is a thin adapter (G2); one Service / Bleve / SQLite per workspace (G3).

## 1. Scenario & assumptions

1. A git repository is the **source of truth** for the notes. It is written and curated by
   actors *outside* GoMental — LLM drafting plus a human review process (PRs, approvals).
2. GoMental is a **viewer**: it reads the curated content, builds its graph/search/inference
   over it, and serves humans + LLMs. It does not author back into the repo.

## 2. Decision: one server in front of one working copy (not local-copy-per-client)

For this scenario the recommended topology is **server mode against a single git working
copy** — "a read replica of git with a graph on top" — rather than every client cloning and
indexing locally. Reasoning (strongest first):

1. **Index cost is paid once.** The SQLite graph + Bleve index + soft-link inference (and the
   in-memory corpus index inference needs) are built once on the server and shared by all
   viewers. Local-per-client rebuilds the whole index on every machine — N× CPU/RAM/disk.
2. **One version everyone agrees on.** The wiki's value is the *shared* link graph (backlinks,
   inferred links, neighborhoods). Local clones drift to different commits, so their graphs
   disagree. A single working copy at a single commit gives every viewer identical content
   *and* identical derived relationships.
3. **Sync, credentials, and remote load are centralized.** One fetcher, one reconcile, one
   webhook endpoint, one place holding repo credentials — not N clients polling the remote.
4. **Thin clients.** The runtime-transport SPA already serves desktop + browser from one
   embedded bundle; browser viewers need only a URL.
5. **Read-mostly makes server mode *easy* here.** Server mode is normally hard because of
   multi-writer concurrency (the optimistic-concurrency / per-note-lock machinery). In a
   viewer scenario nobody writes through GoMental, so that cost disappears.

**Local desktop remains supported** for the offline/solo power user (same binary, a personal
clone, manual "pull"). It is not the default for a shared, curated wiki.
Decision rule: *many viewers who must agree on a version → server; one offline viewer → local.*

## 3. Key architectural fact that shapes the design

The server **already runs a polling workspace watcher**. `application.Service.OpenWorkspace`
(`internal/application/service.go:396`) calls `startWatcherLocked`, which runs
`platform.WorkspaceWatcher` (snapshot + `(mtime,size)` diff every 750 ms). Its changesets flow
through `processWorkspaceChanges` → **incremental** projection updates (`updateIncrementalProjections`)
→ SSE events `note:updated` / `note:deleted` / `graph:updated`, and mark the inference worker
dirty. The scanner already excludes the metadata dir (`workspace.IsMetadataPath`).

**Consequence:** once something advances the git working copy on disk, the existing watcher
reconciles content and notifies clients — for free, incrementally. The git integration
therefore does **not** need to compute diffs or touch the reconcile path. It only needs to
*advance the working copy* and surface git-level state. This keeps `gitsync` almost fully
decoupled and low-risk.

Untracked-file safety: `git reset --hard` only rewrites tracked files, so the untracked
`.workspace/` metadata dir survives. The only dangerous operation is `git clean`, which we
always run as `git clean -fd -e <metadataDir>` (never `-x`) so the derived index is preserved.
Out-of-tree index relocation is therefore **optional** (Phase E, deferred), not required.

## 4. Work breakdown — conflict-free tracks

Tracks are partitioned by **file ownership** so they can be built in parallel without
collisions. Track D (integration) owns the shared wiring and depends on Track A's public API,
which is frozen in §5 below.

| Track | Owns (only) | Depends on |
|------|-------------|-----------|
| **A — `internal/gitsync`** | `internal/gitsync/**` (all new) | §5 contract |
| **B — frontend git-viewer UX** | `frontend/src/**` | §5 contracts |
| **C — docs & ops** | `docs/GIT_SYNC.md`, `deploy/*` (new), `skills/git-viewer-mode.md` | §5 contracts |
| **D — Go integration** | `internal/serverconfig/*`, `main.go`, `internal/httpapi/**` | Track A |

## 5. Frozen contracts (so tracks don't need to negotiate)

### 5.1 `internal/gitsync` public API (Track A)

```go
package gitsync

// Runner executes git subcommands in a working directory. execRunner (default)
// shells out to the `git` binary; tests inject a fake.
type Runner interface {
    Run(ctx context.Context, dir string, args ...string) (stdout string, err error)
}

type Config struct {
    Remote      string        // e.g. https://github.com/org/wiki.git (required)
    Ref         string        // branch/tag to track; default "main"
    Dir         string        // working-copy path == workspace root (required)
    MetadataDir string        // untracked dir to protect from clean; default ".workspace"
    Runner      Runner        // nil → execRunner
    Notify      func(name string, payload any) // nil → no-op; hub.Publish in prod
    Now         func() time.Time               // nil → time.Now (tests inject)
}

type Result struct {
    Fetched    bool
    OldCommit  string
    NewCommit  string
    Changed    []string // repo-relative paths added/modified (informational)
    Deleted    []string // repo-relative paths deleted (informational)
    Renamed    [][2]string
}

type Status struct {
    Remote       string
    Ref          string
    Commit       string     // current HEAD short SHA ("" if not yet cloned)
    LastSyncAt   *time.Time
    LastError    string
    Syncing      bool
}

type Manager struct { /* holds Config + mutex + last Status */ }

func New(cfg Config) (*Manager, error)          // validates Remote/Dir; defaults Ref/MetadataDir
func (m *Manager) Ensure(ctx) error             // clone if Dir is empty/absent; else verify it's a repo for Remote
func (m *Manager) Sync(ctx) (Result, error)     // fetch + reset --hard origin/<ref> + clean -fd -e <meta>; mutex-serialized; emits git:synced / git:sync-error via Notify
func (m *Manager) Status() Status               // snapshot (thread-safe)
func (m *Manager) RunPoll(ctx, interval)        // ticker → Sync until ctx done; interval<=0 returns immediately
```

Notes for Track A:
- Emit through `Notify`: on success `Notify("git:synced", Result-derived map{commit, oldCommit, changed:int, deleted:int})`; on failure `Notify("git:sync-error", map{error})`.
- `Sync` is mutex-serialized; a concurrent call while `Syncing` returns the same in-flight
  result or a `busy` error (your choice — document it).
- Parse `git diff --name-status <old> <new> -z` (or newline form) for Changed/Deleted/Renamed.
  These are informational only (the watcher does the real reconcile); still populate them for
  the webhook response / logging.
- Tests: create a temp **bare** remote + a working clone using the real `git` binary; skip the
  whole test file when `git` is not on PATH (`t.Skip`). Cover: Ensure clones; Sync no-op when
  up to date; Sync detects an upstream commit (changed + deleted); `clean` preserves a file
  created under `MetadataDir`.

### 5.2 serverconfig additions (Track D)

New `Config`/`Options` fields + flags + env (precedence flag > env > file > default):

| Field | Flag | Env | Default |
|------|------|-----|---------|
| `GitRemote` | `--git-remote` | `GOMENTAL_GIT_REMOTE` | "" (git mode off) |
| `GitRef` | `--git-ref` | `GOMENTAL_GIT_REF` | `main` |
| `GitPollInterval` | `--git-poll` | `GOMENTAL_GIT_POLL` | `0` (off; webhook-driven) |
| `GitWebhookSecret` | `--git-webhook-secret` | `GOMENTAL_GIT_WEBHOOK_SECRET` | "" |
| `ReadOnly` | `--read-only` | `GOMENTAL_READ_ONLY` | **true when `GitRemote` set**, else false |

Validation: when `GitRemote != ""`, the workspace dir need **not** pre-exist (Ensure clones it);
relax the "must be an existing dir" check accordingly (create parent if needed). `--git-poll`
accepts a Go duration ("30s", "5m").

### 5.3 HTTP endpoints & events (Tracks D + B)

- `POST /api/git/sync` — trigger a sync. Auth: admin API key **OR** a valid webhook secret
  (header `X-GoMental-Token: <secret>` or `?token=`), so a git host webhook can call it without
  an API key. Returns `{ok, oldCommit, newCommit, changed, deleted}`. 404 when git mode is off.
- `GET /api/git/status` — viewer. Returns the `Status` (see 5.1) as JSON `{remote, ref, commit,
  lastSyncAt, lastError, syncing}`. 404 when git mode is off.
- `/api/events` (existing) additionally carries `git:synced` and `git:sync-error` (generic
  name/payload passthrough — no server changes needed beyond emitting).
- `/api/info` gains two fields: `readOnly: bool` and `git: {remote, ref, commit, lastSyncAt,
  lastError, syncing} | null`.

### 5.4 Read-only enforcement (Track D)

When `cfg.ReadOnly` is true, **content-mutating** routes return `403 {code:"workspace.read_only"}`:
`POST /api/notes`, `PUT /api/notes/{id...}`, `DELETE /api/notes/{id...}`, `POST /api/import`,
`POST /api/assets/{id...}`; and MCP write tools in `handleMCP` (reuse `mcp.IsWriteTool`).
**Still writable** (local view state, stored under `.workspace`, not the git tree): graph
layout, ui-state. Admin ops `POST /api/rebuild` and `POST /api/git/sync` remain available.

### 5.5 Frontend (Track B) — consume, don't invent

- Extend `AppInfoWithMode` in `frontend/src/transport/types.ts` with `readOnly?: boolean` and
  `git?: {remote; ref; commit; lastSyncAt; lastError; syncing} | null`.
- A **read-only banner** shown when `info.readOnly` (concise: "Read-only — content is managed in
  git"). A small **git status chip** (ref + short commit; tooltip with lastSyncAt/lastError).
- Subscribe to `git:synced` → toast + refresh the note list / current note (the backend already
  emits `note:updated`/`graph:updated` for the actual content changes, so lean on those for
  data invalidation; `git:synced` is the human-facing "just pulled" signal).
- When `readOnly`, hide/disable authoring affordances (new note, edit, delete, import). Keep it
  minimal and typecheck-green; a full dist rebuild (`npm run build`) is a separate release step.

## 6. Runtime wiring (Track D, `main.go` runServe)

1. Load cfg. If `cfg.GitRemote != ""`:
   - `mgr, _ := gitsync.New(gitsync.Config{Remote, Ref, Dir: cfg.WorkspaceRoot, Notify: host.Hub().Publish})`
   - `mgr.Ensure(ctx)` **before** `host.OpenWorkspace` (clone must exist before the watcher starts).
   - initial `mgr.Sync(ctx)` (best-effort; log on error).
2. `host.OpenWorkspace(ctx, cfg.WorkspaceRoot)` (unchanged; starts the watcher over the checkout).
3. If `cfg.GitPollInterval > 0`: `go mgr.RunPoll(ctx, cfg.GitPollInterval)`.
4. Pass `mgr`, `cfg.ReadOnly`, `cfg.GitWebhookSecret` into `httpapi.Options`.
5. Help text (`printUsage`) documents the new flags + the server-vs-local recommendation.

## 7. Phases & status

- **A. `internal/gitsync`** — package + tests. _(Track A)_
- **B. Frontend git-viewer UX** — banner, chip, event, read-only affordances. _(Track B)_
- **C. Docs & ops** — `docs/GIT_SYNC.md`, deploy example, skill. _(Track C)_
- **D. Go integration** — config, main wiring, endpoints, read-only gating, `/api/info`. _(Track D)_
- **E. (deferred, optional) Out-of-tree index** — relocate `.workspace` via a configurable
  index dir so the working copy is fully disposable and the index survives re-clone. Only if
  real deployments want it; the `clean -e` guard covers correctness meanwhile.

## 8. Risks / edge cases

- **Non-fast-forward / force-push** on the tracked ref: `reset --hard origin/<ref>` handles it
  (we always match the remote), and the watcher reconciles whatever changed. Log the jump.
- **Large diffs** (bulk import): the watcher's incremental path handles many files; a full
  `POST /api/rebuild` remains the escape hatch.
- **Renames**: git may report them; the watcher sees them as delete+add of note IDs, so
  backlinks re-resolve. `Result.Renamed` is informational.
- **Credentials**: rely on the ambient git credential helper / a `https://<token>@…` remote /
  SSH agent on the server — out of scope to manage in-process.
- **`.workspace` leaking into git**: prevented by `clean -e <metadataDir>` + never `-x`; the
  external repo is not required to gitignore our dir.
- **Concurrent syncs**: `Manager.Sync` is mutex-serialized.
