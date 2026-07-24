# GoMental Server, Multi-User & Agent API Plan

Last updated: 2026-07-18

> **Status: COMPLETE.** Phases 16–24 are implemented and green (`go test ./...`,
> `npm run typecheck`). One product decision was applied: the auth phase (20/22)
> ships **trust-all on LAN** by default — the identity/role/audit *mechanisms* are
> built and the enforcement path is tested, but nothing is rejected unless a real
> authenticator is enabled. See `RUNNING_STATUS.md` for the per-phase record and
> `docs/DEPLOYMENT.md` for operating the server.

This plan drives execution of three additive capabilities on top of the existing
local-first desktop app:

1. **HTTP server mode** — serve the wiki to a browser over the network.
2. **Multi-user support** — a team can search/view and add/edit notes concurrently.
3. **LLM agent API** — a first-class API (MCP + REST) so AI coding agents can
   search, read, create, and edit notes.

It continues the phase numbering from `PROJECT_PLAN.md` / `RUNNING_STATUS.md`
(desktop work ends at Phase 15). Server/collaboration work begins at **Phase 16**.

---

## 0. Guiding Principles (Guardrails)

These are non-negotiable constraints that every phase must respect.

- **G1 — Server mode is additive, never a replacement.** The existing Wails
  desktop build, its native workspace picker, embedded assets, and offline
  single-user behavior must keep working unchanged. Desktop is the default; server
  is opt-in via a subcommand.
- **G2 — One core, two (three) front doors.** All business logic stays in
  `internal/application.Service`. Wails bindings ([app.go](../app.go)), the HTTP
  API, and the agent API are all thin adapters over the *same* `Service` instance.
  No business logic leaks into any adapter.
- **G3 — Single server process owns the workspace.** Bleve and SQLite are
  single-process stores (`bleve.Open` takes an exclusive lock; the SQLite graph DB
  is one file). The server runs exactly **one** `Service` per workspace and funnels
  all users through it. Horizontal scaling of the write path is explicitly out of
  scope.
- **G4 — OKF files remain the source of truth.** Everything added (auth, audit,
  sessions, API keys) is derived/operational state stored under the workspace
  metadata dir (`.workspace/`) or a separate server config dir — never mixed into
  note content in a way that breaks the desktop app or a plain checkout.
- **G5 — Security is a feature, not a phase-end afterthought.** Network exposure
  changes the threat model. Path-traversal guards already exist
  (`ensureInside`, `NormalizeNoteID`); auth, transport security, request limits,
  and SSRF review for `ImportURL` are in-scope from the moment a socket opens.
- **G6 — Every phase ships green.** `go test ./...` and frontend `npm run
  typecheck` pass at the end of each phase. Desktop app still launches.

---

## 1. Target Architecture

```text
                       ┌─────────────────────────────────────────────┐
                       │            internal/application.Service       │
                       │  (transport-agnostic core: notes, search,     │
                       │   graph, import, projections, events)         │
                       └───────────────▲───────────────▲──────────────┘
                                       │               │
             EventSink callback  ──────┤               ├────── EventSink callback
                                       │               │
        ┌──────────────────────┐  ┌────┴─────┐   ┌─────┴───────────────────────┐
        │  Wails bindings       │  │ HTTP API │   │  Agent API                  │
        │  (app.go, desktop)    │  │ (REST +  │   │  (MCP server + REST + keys) │
        │  native dialogs       │  │  SSE/WS) │   │                             │
        └──────────┬───────────┘  └────┬─────┘   └─────┬───────────────────────┘
                   │                    │               │
             WebView (desktop)     Browser (SPA)   Claude Code / Cursor / agents
```

**New Go packages (proposed):**

| Package | Responsibility |
| --- | --- |
| `internal/apphost` | Front-door-agnostic host: constructs `Service`, owns the event fan-out hub, abstracts "environment" (desktop vs headless), gates desktop-only capabilities. |
| `internal/httpapi` | REST router, handlers, `AppError`→HTTP mapping, DTO (re)use, SSE/WebSocket event stream, static SPA serving. |
| `internal/auth` | User model, credential store, sessions/tokens, API keys, role checks, middleware. |
| `internal/agentapi` (or `cmd`-level) | MCP server exposing wiki tools; may reuse `httpapi` handlers. |
| `internal/serverconfig` | Server config load/validate (addr, workspace path, TLS, auth mode). |

**New entrypoint:** `gomental serve` — a subcommand added to
[main.go](../main.go). With no subcommand, `main.go` runs the Wails desktop app
exactly as today.

---

## 2. Phase Overview & Dependency Graph

```text
Phase 16 (core host refactor) ──┬──> Phase 17 (HTTP API) ──> Phase 18 (browser SPA)
                                │            │
                                │            ├──> Phase 22 (agent REST + keys) ──> Phase 23 (MCP)
                                │            │
Phase 19 (concurrency) ─────────┴────────────┤
                                             │
Phase 20 (identity/auth) <───────────────────┘  (auth needed before real team exposure)
                                             │
Phase 21 (realtime multi-user UX) <──────────┘
Phase 24 (packaging/deploy/ops) <── all
```

| Phase | Title | Track | Depends on | Est. |
| --- | --- | --- | --- | --- |
| 16 | Transport-agnostic host & headless bootstrap | Foundation | — | S–M |
| 17 | HTTP API layer (REST + event stream) | Server | 16 | M |
| 18 | Browser SPA adaptation | Server | 17 | M |
| 19 | Safe concurrent writes (optimistic concurrency) | Multi-user | 16 | S–M |
| 20 | Identity, authentication & authorization | Multi-user | 17, 19 | L |
| 21 | Real-time multi-user UX (presence, live edits, conflicts) | Multi-user | 18, 19, 20 | M |
| 22 | Agent REST API + API keys | Agent | 17, 19 | S–M |
| 23 | MCP server for coding agents | Agent | 22 | M |
| 24 | Packaging, deployment, hardening & ops | Cross-cutting | 17–23 | M |

Effort key: S ≈ 1–2 days, M ≈ 3–5 days, L ≈ 1–2 weeks (single engineer, rough).

Minimum viable "team wiki in a browser": **16 → 17 → 18 → 19 → 20**.
Minimum viable "agents can use the wiki": **16 → 17 → 22 (→ 23)**.

---

## Phase 16 — Transport-Agnostic Host & Headless Bootstrap

**Goal:** Make the core runnable without Wails, without changing desktop behavior.

### Scope
- Introduce `internal/apphost` with a `Host` that:
  - constructs and owns the `application.Service`,
  - exposes an **event hub** (fan-out) so many subscribers (WebView, N browser
    SSE clients) receive the same events. Today `EventSink` is a single callback
    ([app.go:127](../app.go)); generalize to publish→multiple subscribers.
  - carries an `Environment` capability set that flags whether native dialogs are
    available.
- Refactor desktop wiring: `App` (app.go) becomes a Wails adapter that registers
  its WebView emitter as one subscriber of the hub. `wailsruntime.EventsEmit`
  stays behind the desktop adapter only.
- Add a **headless open path**: open a workspace by explicit path (config/flag)
  instead of via `SelectWorkspaceDirectory`. `OpenWorkspace(ctx, root)` already
  supports this — just needs a non-dialog caller.
- Gate desktop-only methods: `SelectWorkspaceDirectory` and any
  `wailsruntime.OpenDirectoryDialog` usage must be behind the `Environment`
  capability and return a clear "not available in server mode" error otherwise.

### Files
- New: `internal/apphost/host.go`, `internal/apphost/events.go`.
- Edit: [app.go](../app.go) (become a thin Wails subscriber), [main.go](../main.go)
  (no behavior change yet; prepare for subcommand dispatch).
- No change to `internal/application/service.go` logic (only how `events` is wired).

### Acceptance criteria
- Desktop app launches and behaves identically (manual smoke + existing tests).
- A Go test can construct a `Host`, open a fixed workspace path, subscribe two
  event listeners, save a note, and observe **both** listeners receive
  `note:updated`.
- `go test ./...` and `npm run typecheck` green.

### Risks
- Event fan-out concurrency (subscribers joining/leaving mid-emit). Mitigate with a
  simple mutex-guarded subscriber registry + buffered per-subscriber channels and a
  slow-consumer drop policy (mirrors the existing watcher backpressure pattern in
  `startWatcherLocked`).

---

## Phase 17 — HTTP API Layer (REST + Event Stream)

**Goal:** Expose the full `Service` surface over HTTP and serve the SPA.

### Scope
- New `internal/httpapi` with a router (stdlib `net/http` + `http.ServeMux`, or
  `echo` which is already a transitive dep — decide in Phase 16; prefer stdlib to
  avoid a new direct dep).
- **Endpoint map** (all JSON, DTOs reused verbatim from `application`):

  | Method + Path | Service call | Notes |
  | --- | --- | --- |
  | `POST /api/workspace/open` | `OpenWorkspace` | server-mode: restricted to configured root(s) |
  | `GET /api/notes` | `ListNotes` | |
  | `GET /api/notes/{id}` | `ReadNote` | returns `ModifiedAt` for concurrency |
  | `PUT /api/notes/{id}` | `SaveNote` | honors `If-Match` (Phase 19) |
  | `DELETE /api/notes/{id}` | `DeleteNote` | |
  | `POST /api/notes/import` | `ImportURL` | SSRF review required |
  | `POST /api/notes/{id}/assets` | `SaveNoteAsset` | multipart or base64 |
  | `GET /api/notes/{id}/assets?path=` | `LoadNoteAssetDataURL` | |
  | `POST /api/search` | `Search` | primary agent/RAG entry |
  | `POST /api/graph` | `FullGraph` | |
  | `GET /api/notes/{id}/neighborhood?depth=` | `Neighborhood` | |
  | `GET /api/notes/{id}/backlinks` | `Backlinks` | |
  | `GET/PUT /api/graph/layout` | `Load/SaveGraphLayout` | |
  | `POST /api/rebuild` | `Rebuild` | admin-gated later |
  | `GET /api/recent` | `RecentWorkspaces` | desktop-oriented; may hide in server |
  | `GET/PUT /api/ui-state` | `Load/SaveUIState` | per-user later (Phase 20) |
  | `GET /api/info` | `Info` | |
  | `GET /api/events` | event hub | SSE stream |
  | `GET /` + assets | embedded SPA | `http.FileServer` over `frontend/dist` |

- **Error mapping:** central middleware turns `application.AppError` into HTTP
  status + `{code,message,detail}` body. Suggested mapping: `*.not_open`→409,
  `*.invalid_*`→400, `edit.external_conflict`→412, traversal/escape→400,
  unexpected→500. Keep the `code` string so clients branch on it.
- **Event stream:** `/api/events` as SSE (simplest; one-way server→client is all
  the current events need). WebSocket (`gorilla/websocket`, already transitive) is
  the fallback if bidirectional is needed later. Each connection = one hub
  subscriber; clean up on disconnect.
- **Static serving:** serve embedded `frontend/dist` (reuse `embed.FS`); SPA
  fallback to `index.html` for client routes.
- **`gomental serve` subcommand** in main.go: parse flags/config
  (`--workspace`, `--addr`, later `--tls-*`, `--auth`), build the `apphost.Host`
  in headless mode, open the workspace, start the HTTP server. Desktop path
  untouched when no subcommand is given.

### Files
- New: `internal/httpapi/*.go`, `internal/serverconfig/config.go`,
  `cmd`/main.go subcommand dispatch.
- Edit: [main.go](../main.go).

### Acceptance criteria
- `gomental serve --workspace <path> --addr :8080` starts; `curl` against each
  endpoint returns correct JSON; `GET /` serves the SPA shell.
- Saving a note via `PUT` triggers an SSE `note:updated` event observed by a
  second connected client.
- Desktop build still runs (G1). Server mode requires **no** code path change in
  `service.go`.
- Integration test: httptest server + real temp workspace exercises
  list/read/save/search/delete happy paths and one error (404/409).

### Risks
- Large asset/import bodies → enforce request size limits (mirror existing 25 MB
  asset / 8 MB import caps).
- `ImportURL` server-side fetch is an **SSRF vector** now that it's network-callable
  — add allow/deny for internal IP ranges, keep the existing timeout, and make it
  admin/authenticated-only. Track as a security item, do not defer silently.

---

## Phase 18 — Browser SPA Adaptation

**Goal:** The React app runs in an ordinary browser against the HTTP API, with the
desktop (Wails-binding) build still intact.

### Scope
- **Transport abstraction in the frontend.** Today components import generated
  bindings directly:
  - `../wailsjs/go/main/App` — [App.tsx:24](../frontend/src/App.tsx),
    [GraphPanel.tsx:5](../frontend/src/GraphPanel.tsx),
    [MdxNoteEditor.tsx:18](../frontend/src/MdxNoteEditor.tsx),
  - `EventsOn` from `../wailsjs/runtime/runtime` — [App.tsx:25](../frontend/src/App.tsx).

  Introduce a single `src/transport/` module exporting the same function names and
  signatures (all Promise-returning) plus an `onEvent(name, cb)` helper. Two
  implementations:
  - `transport/wails.ts` — re-exports the generated bindings + `EventsOn`
    (desktop, default; zero behavior change).
  - `transport/http.ts` — `fetch`-based calls to `/api/*` + an SSE
    `EventSource('/api/events')` for events.
  - Selection at build time via a Vite env flag (`VITE_TRANSPORT=http|wails`), so
    the desktop bundle keeps using Wails and a new browser bundle uses HTTP.
- Migrate the ~4 files to import from `src/transport/` instead of `wailsjs/*`.
  Component logic is untouched — signatures match.
- **Desktop-only UI gating:** the native folder picker
  (`SelectWorkspaceDirectory`) has no browser equivalent. In HTTP transport,
  replace with either (a) fixed server-configured workspace (no picker), or
  (b) a "choose from server-allowed workspaces" list. Recommend (a) for v1.
- Vite build target for the browser bundle (served by the Go server); keep the
  existing desktop build pipeline (`wails.json` direct-Vite invocation) unchanged.

### Files
- New: `frontend/src/transport/index.ts`, `wails.ts`, `http.ts`, `events.ts`.
- Edit: `App.tsx`, `GraphPanel.tsx`, `MdxNoteEditor.tsx`, `CodeMirrorEditor.tsx`
  (import path swaps only).
- Edit: `vite.config.ts` (env-driven transport, browser build output).

### Acceptance criteria
- `VITE_TRANSPORT=wails` build → desktop app works exactly as before.
- `VITE_TRANSPORT=http` build served by `gomental serve` → browse, open, edit,
  save, search, and graph all work in Chrome/Firefox; live updates arrive via SSE.
- `npm run typecheck` green for both transports.

### Risks
- MDX/CodeMirror asset loading (`LoadNoteAssetDataURL`) returns data URLs — works
  over HTTP unchanged, but verify large images don't blow request limits.
- SSE reconnection/backoff on network blips — implement basic auto-reconnect in
  `transport/events.ts`.

---

## Phase 19 — Safe Concurrent Writes (Optimistic Concurrency)

**Goal:** Concurrent editors (humans and agents) cannot silently clobber each
other. This is a prerequisite for any real multi-writer use.

### Current gap (verified)
- `SaveNote` ([service.go:317](../internal/application/service.go)) ignores
  versioning — last write wins.
- `domain.FileVersion` (ModTime+Size) **is captured** on `Read`
  ([repository.go:78](../internal/workspace/repository.go)) but never checked on
  save.
- `ErrExternalConflict = "edit.external_conflict"` is **defined but unused**
  ([service.go:36](../internal/application/service.go)).

### Scope
- Add an expected-version field to `SaveNoteRequest` (e.g. `BaseModifiedAt` or an
  opaque `Version` token derived from `FileVersion`).
- In `repository.Save` (or a new `SaveIfUnchanged`), stat the file before write; if
  the on-disk version differs from the expected version, return a conflict that
  `Service` maps to `ErrExternalConflict`.
- HTTP: honor `If-Match`/`If-Unmodified-Since` semantics; return **412 Precondition
  Failed** with the current server version so clients can merge/retry.
- Add a **per-note write lock** (keyed mutex map in `Service` or `apphost`) so
  concurrent saves to the *same* note serialize; different notes stay parallel.
- **Scaling fix (important, can be its own sub-task):** `updateOneProjection`
  ([service.go:1295](../internal/application/service.go)) re-reads and re-parses the
  **entire corpus** on every save to recompute soft links — O(N) disk reads per
  edit. Under team load this is the primary bottleneck. Options: (a) debounce/queue
  soft-link recompute out of the save hot path (reuse the existing background
  soft-link rebuild machinery, `startSoftLinkRebuildLocked`), keeping only the
  edited note's hard-link + search update synchronous; (b) cache the parsed corpus
  and invalidate incrementally. Recommend (a) for v1.

### Files
- Edit: `internal/application/service.go` (SaveNote, request DTO, per-note lock,
  soft-link debounce), `internal/workspace/repository.go` (version-checked save),
  `internal/httpapi` (If-Match handling).

### Acceptance criteria
- Two concurrent saves with the same base version: one succeeds, the other returns
  `edit.external_conflict` / HTTP 412 with the current version.
- Save latency no longer scales linearly with corpus size (benchmark before/after
  on a synthetic 5k-note workspace — ties into Phase 15 benchmarks).
- Desktop save path still works (it can send the version it read).

### Risks
- The desktop app must start sending the expected version too, or opt into
  force-save; keep a `force` escape hatch for the single-user desktop case.

---

## Phase 20 — Identity, Authentication & Authorization

**Goal:** Know who a request is, and enforce what they may do. Largest new surface.

### Current gap (verified)
- No user/session/permission/auth concept exists anywhere in the codebase.
- Notes have no author/attribution field.

### Scope
- **User & credential store** (`internal/auth`): users with id, display name,
  role. Store under a server config dir (not in notes). Start with local
  username+password (bcrypt/argon2) or a bearer-token/API-key model; design the
  interface so an OIDC/SSO provider can slot in later without touching call sites.
- **Sessions/tokens:** cookie-based session for the browser SPA; bearer tokens for
  programmatic use (shared with Phase 22 API keys).
- **Roles / authorization:** minimum viable set — `viewer` (search/read/graph),
  `editor` (+ create/edit/delete/import/asset), `admin` (+ rebuild, workspace
  open, user management). Enforce via HTTP middleware keyed to the endpoint map in
  Phase 17. Keep the *core Service unaware of auth* (G2) — authorization is an
  adapter concern.
- **Attribution & audit:** stamp author + timestamp on writes. Two options:
  (a) store in OKF frontmatter/metadata (visible, travels with the note, must stay
  desktop-compatible), or (b) a side audit log under `.workspace/`. Recommend an
  **append-only audit log** (`who / when / action / note id / version`) plus
  optional OKF `author` metadata. Decide in a design spike.
- **Per-user UI state:** `Load/SaveUIState` currently writes one shared file
  ([service.go:637](../internal/application/service.go)); namespace it per user in
  server mode.
- **Login UI** in the SPA (only under HTTP transport; desktop stays
  auth-less/single-user).

### Files
- New: `internal/auth/*`, auth middleware in `internal/httpapi`, login views in
  `frontend/src`.
- Edit: `service.go` (thread an author/actor through write methods *as data*, not
  as an auth dependency), UI-state namespacing.

### Acceptance criteria
- Unauthenticated request to a protected endpoint → 401; wrong role → 403.
- A viewer cannot save; an editor can; an admin can rebuild.
- Every write appears in the audit log with the acting user and note version.
- Desktop mode unaffected (auth disabled by default there).

### Risks
- Scope creep (SSO, granular per-note ACLs, groups). **Out of scope for v1** —
  ship coarse roles first; note the extension point.
- Secret handling: password hashes, token storage, session cookie flags
  (HttpOnly, Secure, SameSite). Requires the TLS story from Phase 24.

---

## Phase 21 — Real-Time Multi-User UX

**Goal:** A team editing together sees each other's changes and resolves conflicts
gracefully.

### Scope
- **Live updates (already 80% there):** the file watcher + event hub already emit
  `note:updated` / `note:deleted` / `graph:updated`. Broadcast these to all SSE
  clients (Phase 17 does the plumbing) so a note saved by user A refreshes user B's
  list/graph. Verify the watcher-driven events (external edits, git pulls) also
  fan out.
- **Conflict UX:** when a save returns 412 (Phase 19), the SPA shows a
  "changed on server" banner with reload/overwrite/diff options. No CRDT.
- **Presence (optional, nice-to-have):** lightweight "N users viewing/editing this
  note" via the event hub. Ephemeral, in-memory.
- **Explicitly out of scope:** simultaneous character-level co-editing
  (OT/CRDT) — a large separate initiative; call it out so expectations are set.

### Acceptance criteria
- User B's note list and open note update within ~1s of user A's save.
- Editing a note that changed underneath you surfaces a conflict banner, never a
  silent overwrite.

### Risks
- Event storms during bulk operations (rebuild, git pull) — coalesce/debounce on
  the client (the backend already debounces watcher changes at 300ms).

---

## Phase 22 — Agent REST API + API Keys

**Goal:** AI coding agents can search/read/create/edit notes over a stable,
authenticated API.

### Scope
- Reuse the Phase 17 endpoints; add **API-key auth** (issued to agents/service
  accounts, tied to a role from Phase 20 — typically `editor`). Keys are separate
  from human sessions and revocable.
- Agent-ergonomic touches:
  - **Idempotent create**: a `create-or-fail` vs `upsert` mode; reuse the
    `uniqueImportedNoteID` collision pattern
    ([service.go:860](../internal/application/service.go)) for auto-naming.
  - **Search is the RAG entry point**: `POST /api/search` already returns ranked
    results with highlighted `Fragments`
    ([service.go:492](../internal/application/service.go)) — document it as the
    primary retrieval tool and ensure limits/pagination are sane for agents.
  - Return note **version tokens** on read so agents participate in optimistic
    concurrency (Phase 19).
- **API documentation:** publish an OpenAPI spec for the `/api` surface (agents and
  humans both benefit).

### Files
- Edit: `internal/auth` (API keys), `internal/httpapi` (key middleware, OpenAPI),
  docs.

### Acceptance criteria
- An agent with an API key can: search → read a hit → edit it with the returned
  version → create a new note — all via `curl`/SDK, no browser session.
- A revoked key is rejected.

### Risks
- Runaway agents: add per-key rate limiting and a max-writes guard.

---

## Phase 23 — MCP Server for Coding Agents

**Goal:** Expose the wiki as Model Context Protocol tools so Claude Code, Cursor,
etc. use it natively as a knowledge source and note store.

### Scope
- Add an MCP server (Go MCP SDK) exposing tools that map onto `Service`:
  - `search_wiki(query, tags?, limit?)` → ranked results + fragments
  - `read_note(id)` → content + version
  - `list_notes(path_prefix?, tag?)`
  - `create_note(id?, content)` → id (idempotent)
  - `edit_note(id, content, base_version)` → new version (optimistic)
  - `backlinks(id)`, `neighborhood(id, depth)` for graph-aware retrieval
- Two delivery modes: (a) **stdio** MCP server subprocess for local agents
  (`gomental mcp --workspace <path>`), reusing the in-process `Service`;
  (b) **HTTP/SSE MCP** fronting the running server (reuses Phase 22 auth) for
  remote/team agents.
- Tool descriptions and schemas tuned for LLM use (clear when-to-use text, stable
  IDs, small structured outputs).

### Files
- New: `internal/agentapi/mcp/*`, `cmd` `mcp` subcommand.

### Acceptance criteria
- Claude Code (or MCP Inspector) connects, lists tools, runs `search_wiki` and
  `create_note` against a temp workspace.
- Concurrent agent + human edits are conflict-safe (shares Phase 19).

### Risks
- MCP SDK maturity/version churn — pin the dependency; keep the tool layer thin
  over `Service` so a SDK swap is cheap.

---

## Phase 24 — Packaging, Deployment, Hardening & Ops

**Goal:** Make server mode deployable and safe to expose.

### Scope
- **Config & secrets:** `serverconfig` finalizes addr, workspace root(s), TLS
  cert/key (or ACME), auth mode, allowed import ranges, rate limits. Support env +
  file.
- **Transport security:** TLS termination (native or documented reverse-proxy
  pattern). Secure cookie flags. HSTS.
- **Hardening:** request size limits, timeouts, rate limiting, CORS policy for the
  SPA, SSRF guard for `ImportURL`, audit-log rotation.
- **Packaging:** the single binary already contains both modes (embedded SPA +
  desktop). Provide a container image and a systemd/service example for headless
  deploys. Desktop installer path unchanged.
- **Observability:** structured logs (extend the existing
  `logProjectionRecovery` pattern), health/readiness endpoints, basic metrics.
- **Backup guidance:** the workspace dir (notes + `.workspace/`) is the backup
  unit; document it.

### Acceptance criteria
- Server runs behind TLS with auth on, passes a basic security checklist
  (authn/authz, transport, input limits, SSRF), and survives a soak test with
  multiple concurrent users + an agent.
- Desktop distribution unaffected.

---

## 3. Cross-Cutting Concerns

- **Testing strategy:** unit tests per package (existing pattern), `httptest`
  integration tests for the API, a concurrency test harness for Phase 19, and an
  end-to-end smoke (server up → SPA loads → edit → SSE propagation). Continue the
  repo's strong test-coverage convention.
- **Docs to update as phases land:** `RUNNING_STATUS.md` (phase table + decisions),
  `REQUIREMENTS.md` (new REQs for server/multi-user/agent), `DOMAIN_MODEL.md` (new
  Identity/Session/AuditLog concepts), and this file.
- **Config precedence:** desktop = no flags → Wails; `serve`/`mcp` subcommands =
  headless. A single misconfiguration must never silently downgrade auth.
- **Backwards/interop:** a workspace edited by the desktop app and by the server
  must stay mutually readable (G4) — server-only state stays out of note content
  or in optional OKF metadata only.

## 4. Risk Register (top items)

| Risk | Phase | Severity | Mitigation |
| --- | --- | --- | --- |
| Silent data loss on concurrent edit | 19 | High | Optimistic concurrency before any multi-writer exposure |
| Full-corpus reparse per save doesn't scale | 19 | High | Move soft-link recompute off the save hot path |
| SSRF via `ImportURL` once network-exposed | 17/24 | High | Auth-gate + internal-range deny + timeout |
| Auth scope creep (SSO, per-note ACL) | 20 | Med | Ship coarse roles v1; document extension points |
| Bleve/SQLite single-process assumption violated | G3 | High | Enforce one Service per workspace; document no horizontal write scaling |
| Desktop regression from refactor | 16/18 | Med | G1/G6 gates; keep Wails transport as default build |
| MCP SDK churn | 23 | Low | Thin tool layer over Service; pin version |

## 5. Out of Scope (v1)

- Character-level real-time co-editing (CRDT/OT).
- Horizontal scaling / multiple server instances over one workspace.
- Per-note / per-folder ACLs and user groups (coarse roles only).
- Federated/SSO identity (interface allows it later).
- Mobile-native clients (responsive browser SPA only).

## 6. Suggested Execution Order (TL;DR)

1. **16 → 17 → 18** — get the wiki in a browser (still trust-all on a LAN).
2. **19** — make concurrent writing safe (also unblocks agents).
3. **20** — add auth/roles before any real/untrusted exposure.
4. **22 → 23** — turn on the agent API and MCP.
5. **21** — polish the live multi-user experience.
6. **24** — harden and deploy.
