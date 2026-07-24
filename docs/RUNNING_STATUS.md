# GoMental Running Status

Last updated: 2026-07-18

This document tracks implementation progress, architecture decisions, open issues, and handoff notes across phases and agents. Keep it current whenever a phase changes status, a decision is made, or a blocker is discovered.

## Current Phase

Server/collaboration track (Phases 16–24 from `SERVER_COLLAB_PLAN.md`) is **complete**. GoMental now runs as a multi-user browser wiki and an AI-agent knowledge store, in addition to the unchanged Wails desktop app.

- **16–18**: transport-agnostic host + event hub; HTTP REST + SSE API with `gomental serve`; browser SPA with runtime-selected transport (one bundle serves desktop and browser).
- **19**: optimistic concurrency (version tokens, `If-Match`/412, per-note locks) + save hot-path no longer reparses the corpus.
- **20**: identity/authorization *mechanisms* with a **trust-all** default (product decision) — roles, pluggable authenticator, append-only audit log; enforcement path proven but off by default.
- **21**: conflict banner + live multi-user updates in the SPA.
- **22**: agent REST API + revocable API keys + OpenAPI + rate limits.
- **23**: dependency-free stdio MCP server (`gomental mcp`).
- **24**: TLS/CORS/HSTS, config file, audit rotation, health/ready/metrics, Dockerfile + systemd + `docs/DEPLOYMENT.md`.

Note: `go`/`node` are not on PATH by default on the dev box; a portable Go 1.25 lives at `C:\atera\gotool` — `source /c/atera/gotool/env.sh` before `go`/`npm`. Phase 15 (benchmarks) remains open from the desktop track.

## Phase Status

| Phase | Name | Status | Notes |
| --- | --- | --- | --- |
| 0 | Architecture Verification | Complete | Wails, Bleve, SQLite, OKF source/profile, command layout, metadata path, note ID policy, and Wails build workflow are verified or decided. |
| 1 | Repository And Wails Scaffolding | Complete | Wails root entrypoint, React/TypeScript frontend shell, direct Vite build/dev watcher, direct npm install helper, generated bindings, production build, and live `wails dev` app-window verification are working. |
| 2 | Workspace Path Model And Note Repository | Complete | Implemented workspace validation, note ID/path normalization, scan exclusions, file-backed note repository, rename, recent workspace persistence, and focused tests. |
| 3 | OKF Codec, Parsing, And Link Resolution | Complete | Implemented OKF metadata codec, parser, headings/tags/plain-text extraction, hard Markdown/wiki link extraction, resolver, unresolved-link handling, and tests. |
| 4 | Bleve Search Adapter | Complete | Implemented SearchDocument/SearchIndex domain types and Bleve adapter with index, delete, search, rebuild, filters, highlighting, persistence, and tests. |
| 5 | GraphStore, SQLite Adapter, And Link Inference | Complete | Implemented graph domain types, SQLite GraphStore, hard/soft edges, backlinks, neighborhoods, full graph, persistence tests, and deterministic soft-link inference. |
| 6 | Initial Scan And Rebuild Pipeline | Complete | Implemented rebuild service connecting workspace scan, OKF parse/link resolve, Bleve rebuild, SQLite graph rebuild, soft-link inference, progress reporting, state persistence, and tests. |
| 7 | Wails Application APIs | Complete | Implemented application service, Wails-bound APIs, DTO mapping, structured AppError values, progress/update events, UI state, recent workspaces, and tests. |
| 8 | React Shell And File Tree | Complete | Integrated React shell with Wails APIs, native workspace picker, recent workspaces, grouped note tree, note selection/read, backlinks, progress/error state, UI state persistence, and dev smoke check. |
| 9 | CodeMirror OKF Note Editor | Complete | Added editable CodeMirror source-mode OKF editing, new-note creation, save/autosave state, completions, link navigation, and frontend conflict checks. |
| 10 | Search UI | Complete | Added debounced search, stale request handling, highlighted results, tag/path filters, result limits, incremental More loading, and click-to-open. |
| 11 | Graphology And Sigma Integration | Complete | Added Graphology/Sigma rendering, local/global graph modes, filters, click-to-open, hover neighborhood emphasis, reset/refresh controls, and dev/build verification. |
| 12 | ForceAtlas2 Worker And Layout Persistence | Complete | Added ForceAtlas2 worker layout, persisted graph coordinates under `.workspace/layout/graph-layout.json`, layout load/save APIs, and dev/build verification. |
| 13 | Filesystem Watching And Incremental Updates | Complete | Added polling workspace watcher under `internal/platform`, bounded/debounced service event handling, incremental search/graph projection updates for external create/modify/delete, UI refresh events, and focused tests. |
| 14 | Error Recovery And Rebuild Flows | Complete | Added structured recovery/error codes, automatic missing/stale/corrupt projection repair on workspace open, safe rebuild logging under `.workspace/logs/`, error-strip rebuild action, and focused corrupt-state tests. |
| 15 | Benchmarks And Performance Tuning | Ready | Depends on vertical-slice functionality, which is now implemented through recovery flows. |
| 16 | Transport-Agnostic Host And Headless Bootstrap | Complete | Added `internal/apphost` (`Host` + fan-out event `Hub` with slow-consumer drop policy + `Environment` capabilities). Refactored `app.go` into a thin Wails subscriber over the host; `wailsruntime.EventsEmit` is now behind the desktop adapter only. `SelectWorkspaceDirectory` gated behind `Environment.NativeDialogs`. `main.go` extracted `runDesktop()` and prepared subcommand dispatch (desktop remains the default). Fan-out acceptance test: two subscribers both observe `note:updated`. `go test ./...` + `npm run typecheck` green. |
| 17 | HTTP API Layer (REST + Event Stream) | Complete | Added `internal/httpapi` (stdlib `http.ServeMux` method+wildcard routing, DTO reuse, `AppError`→HTTP status mapping, SSE `/api/events`, embedded SPA serving with client-route fallback, recoverer + 40 MiB body-limit middleware, SSRF guard for `ImportURL`, `/api/healthz`) and `internal/serverconfig` (addr + workspace root, env/flag precedence, validation). Added `gomental serve --workspace --addr` subcommand to `main.go` (headless host, graceful shutdown on SIGINT/SIGTERM). Note-scoped routes use `{id...}` wildcards; sub-resources (backlinks/neighborhood/assets/import) get own prefixes to disambiguate slash-containing IDs. httptest integration test covers list/read/save/search/delete + 404 + 403 + SSE `note:updated` fan-out to a second client. Verified end-to-end with `curl`. Desktop path unchanged. |

| 18 | Browser SPA Adaptation | Complete | Added `frontend/src/transport/` (`wails.ts`, `http.ts`, `events.ts`, `index.ts`) exporting one unified binding surface + `onEvent`. Selection is **runtime** by default (`window.go.main.App` present → Wails, else HTTP/SSE via `EventSource('/api/events')`), with `VITE_TRANSPORT=http|wails` as a build-time override — so a SINGLE embedded bundle works in both desktop and browser. Migrated `App.tsx`/`GraphPanel.tsx`/`MdxNoteEditor.tsx` to import bindings from `./transport` and replaced `EventsOn` with `onEvent` (component logic unchanged; model/type imports from `wailsjs/go/models` left intact). HTTP transport encodes note-ID path segments (slashes preserved), unwraps asset `{dataUrl}`, rejects `SelectWorkspaceDirectory`, and throws `TransportError` carrying `{code,detail}`. `npm run typecheck` green for both transports; both `npm run build`s succeed. |
| 19 | Safe Concurrent Writes (Optimistic Concurrency) | Complete | Added opaque version token (`<modUnixNano>-<size>`) to `NoteDTO.Version`; `SaveNoteRequest` gained `BaseVersion`/`Force`. New `repository.SaveIfUnchanged` stats-and-checks before write → `workspace.ErrVersionConflict` → mapped to `ErrExternalConflict`/HTTP **412** (current version returned in ETag). Per-note write lock (keyed `sync.Map` of mutexes) in `Service` serializes same-note saves/deletes while different notes stay parallel. Save hot path no longer reparses the whole corpus: `updateOneProjectionFast` updates only the edited note's search doc + hard links (link resolution uses cheap ID-only list); soft-link inference moved off the hot path via the existing background rebuilder (debounced). SQLite store hardened with WAL + `busy_timeout(10000)` + `foreign_keys` in the DSN so the live store and the background rebuilder no longer collide (`SQLITE_BUSY`). Desktop default (empty BaseVersion) stays last-write-wins. Acceptance tests: concurrent same-base saves → exactly one 412; stale-version + force + unconditional paths; HTTP If-Match 412. |

| 20 | Identity, Authentication & Authorization (trust-all v1) | Complete | Per product decision, v1 posture is **trust-all on LAN**: mechanisms are built but enforcement is OFF by default. Added `internal/auth` — `Role` (viewer<editor<admin), `Actor`, pluggable `Authenticator` interface + `TrustAll` default (returns local admin, `Enforced()=false`), and an append-only JSONL `AuditLog` under `.workspace/audit/audit.log` (Guardrail G4). HTTP layer: `authMiddleware` resolves the actor into request context; per-route `gate(role, …)` keyed to the endpoint map (viewer/editor/admin); write handlers (`save`/`delete`/`import`/`rebuild`) record audit entries (actor + note id + version + result). `serverconfig` gained `AuthMode` (`trustall` default, `--auth`/`$GOMENTAL_AUTH`); `main.go serve` opens the audit log and logs a "not enforced" warning. The enforcement path is proven live by tests (a restrictive stub authenticator → 401 unauthenticated, 403 for a viewer writing) even though trust-all ships as default. Per-user UI-state namespacing is deferred (single-actor under trust-all); noted as the extension point. Desktop unaffected (no HTTP layer). |

| 21 | Real-Time Multi-User UX | Complete | Frontend: version token threaded through `ReadNote`/`SaveNote`/create/import; save sends `baseVersion`. On a `edit.external_conflict` (both transports expose `err.code`) a **conflict banner** offers Reload / Overwrite(force) / Dismiss instead of silent clobber. Live `note:updated` for the open note refreshes transparently when clean, or raises the conflict banner when there are unsaved edits; external delete shows a removal notice. `graph:updated`/`note:*` graph-revision bumps coalesced behind a 280 ms debounce; fixed a latent `projection:repairing/repaired` listener leak. Backend live fan-out (watcher → hub → all SSE clients) already in place from Phase 16/17 and covered by the SSE fan-out test. Presence intentionally out of scope. `npm run typecheck` green. |
| 22 | Agent REST API + API Keys | Complete | `internal/auth/apikey.go`: revocable `APIKeyStore` (SHA-256-hashed secrets at rest under `.workspace/server/api-keys.json`, plaintext shown once) + `BearerAuthenticator` (Bearer/`X-API-Key`; valid key → its actor/role, keyless → trust-all fallback, invalid/revoked → 401; `RequireKey` flips to full enforcement). Agent-ergonomic `POST /api/notes` create with `create`/`upsert`/`unique` modes (`Service.CreateNote`, reuses per-note lock + fast projection; `unique` reuses the import auto-naming). Admin key management (`POST`/`GET /api/keys`, `DELETE /api/keys/{id}`). Per-actor token-bucket rate limiting + stricter write budget (`429`). Embedded OpenAPI 3 spec at `GET /api/openapi.json` documenting the surface + `/api/search` as the RAG entry point + version-token concurrency. Read/save already return version tokens (Phase 19). Acceptance test: agent with a key does search→read→edit-with-version→create over HTTP, and a revoked key is rejected; verified end-to-end with `curl` (key mint → attributed create → audit trail). |

| 23 | MCP Server for Coding Agents | Complete | `internal/agentapi/mcp`: a **dependency-free** stdio MCP server (JSON-RPC 2.0, newline-delimited) reusing the in-process `Service` (Guardrail G2). Handles `initialize`/`notifications/initialized`/`ping`/`tools/list`/`tools/call`. Tools tuned for LLM use: `search_wiki`, `read_note`, `list_notes`, `create_note`, `edit_note` (optimistic — `base_version`, conflict surfaced as `isError`), `backlinks`, `neighborhood`. `gomental mcp --workspace <path>` runs it on stdin/stdout (logs to stderr so they never corrupt the stream). Concurrency-safe: shares Phase 19 per-note locks. Chose a self-contained protocol layer over an external SDK to avoid MCP-SDK churn (Risk Register) while keeping the tool layer thin for a future SDK/Streamable-HTTP swap. HTTP/SSE MCP transport noted as a follow-up. Tests cover initialize/list/call (search→create→read→edit→conflict) + unknown tool/method; verified via real `gomental mcp` stdio session. |

| 24 | Packaging, Deployment, Hardening & Ops | Complete | `serverconfig` finalized: TLS cert/key, CORS allow-list, per-actor rate limits, and a JSON **config file** with precedence flag > env > file > default (validated; TLS all-or-nothing). `serve` flags/env for all of the above. HTTP hardening middleware: metrics recorder, panic recover, baseline **security headers** (nosniff/frame/referrer + **HSTS when TLS**), configurable **CORS** (with preflight), body-size cap, per-actor rate + write limits. **TLS** listener via `ListenAndServeTLS`. Audit log **rotates** at 10 MiB (keeps one generation). Ops endpoints: `/api/healthz` (live), `/api/readyz` (ready when workspace open), `/api/metrics` (Prometheus-style). Packaging: multi-stage `Dockerfile` (CGO-free static binary on distroless; SPA built in a node stage and embedded), `.dockerignore`, `deploy/gomental.service` (hardened systemd unit), `deploy/gomental.example.json`, and `docs/DEPLOYMENT.md` (config table, security posture, TLS/CORS, API keys, MCP, observability, backup = workspace dir incl. `.workspace/`, single-process scaling note). Verified `serve` end-to-end: readyz/metrics/CORS-preflight/security-headers. Desktop distribution unaffected. |

| UI Redesign | Wiki reader/editor visual + interaction redesign | Complete | Applied the `docs/design/` hifi handoff to the React frontend: design tokens (Hanken Grotesk UI / Newsreader serif reading / JetBrains Mono), light+dark themes (persisted to `localStorage:gm-theme`, `data-theme`/`data-accent=iris`), fixed three-pane layout (56px header · 290px sidebar · main · 300px right rail). New/rebuilt components under `frontend/src/ui/` (`icons`, `MarkdownArticle` block renderer with `**bold**`/`` `code` ``/`[[wikilink]]` inline handling + outline/word-count, `CommandPalette` ⌘K, `Toast`, radial-SVG `GraphView`). Sidebar folder-tree/search-results states, breadcrumb + Note/Graph tabs + Source/Preview toggle + Save, right rail On-this-page (scroll-spy)/Details/Linked/Backlinks. All existing backend wiring preserved: optimistic concurrency + conflict banner, SSE live updates, autosave, transport calls. Verified live against `gomental serve` (fonts loaded, tokens applied, tree/article/table/steps/callout/wikilink, palette, graph nodes+edges, theme persist, real backlinks/linked-notes). `npm run typecheck` + `go build`/`go test ./...` green. Note: legacy Sigma `GraphPanel.tsx`/`graphLayoutWorker.ts` are now unused (replaced by `GraphView`). |

## Architecture Decisions

### ADR-0001: Use SQLite Instead Of Cayley For Graph Persistence

Status: Accepted.

Decision: GoMental will not use Cayley for graph persistence. Graph relationships will be stored in SQLite behind the `GraphStore` interface.

Rationale:

- Phase 0 Cayley investigation showed an old latest tag (`v0.7.7`) and dependency/API friction under the current Go toolchain.
- GoMental needs a local, embedded, reliable graph projection rather than a general graph database server.
- SQLite is mature, inspectable, easy to migrate, works well for local desktop apps, and can support hard links, soft links, backlinks, neighborhoods, and filtered full-graph queries through schema and recursive CTEs.
- Keeping SQLite behind `GraphStore` preserves replaceability and prevents SQL schema details from leaking into domain or UI code.

Impacted artifacts:

- `PROJECT_PLAN.md`
- `DOMAIN_MODEL.md`
- `REQUIREMENTS.md`

### ADR-0002: Hard Links And Soft Links

Status: Accepted.

Decision: Explicit OKF-authored note relationships are hard links. Inferred local relationships are soft links stored as rebuildable derived data with score, evidence, algorithm version, and computed timestamp.

Rationale:

- Hard links preserve user intent and are authoritative for backlinks and broken-link reporting.
- Soft links provide discovery without mutating OKF notes.
- Users can promote a soft link into a hard link by explicitly adding an OKF link.

### ADR-0003: SQLite Driver

Status: Accepted.

Decision: Use `modernc.org/sqlite v1.53.0` as the first SQLite driver.

Rationale:

- It works with `CGO_ENABLED=0`, which simplifies Windows desktop builds and future cross-platform packaging.
- The Phase 0 spike succeeded for migration, hard links, soft links, close/reopen, outgoing links, backlinks, and recursive neighborhood queries.
- `mattn/go-sqlite3` remains a fallback candidate if modernc-specific issues appear later, but it requires CGO.

### ADR-0004: Module And Command Layout

Status: Accepted for the first implementation.

Decision: Keep the Go module path as `GoMental` for now and use `cmd/GoMental` as the application command path unless Wails scaffolding requires a different final layout.

Rationale:

- The current placeholder already builds under module `GoMental`.
- `cmd/GoMental` preserves a product-oriented executable name.
- If Wails scaffolding strongly prefers root-level `main.go`, keep the domain/application packages stable and treat the command layout as an adapter detail.

### ADR-0005: Workspace Metadata Directory

Status: Accepted.

Decision: Use `.workspace/` as the workspace-local application metadata directory.

Rationale:

- It was already used throughout the plan.
- It cleanly groups rebuildable projections: search, graph, layout, and state.
- It must be excluded from workspace note scanning.

### ADR-0006: Windows Note ID Case Policy

Status: Accepted for the Windows-first version.

Decision: Note IDs use workspace-relative paths with `/` separators. Preserve actual filename casing for display, but detect ID collisions case-insensitively on Windows.

Rationale:

- This matches normal Windows filesystem expectations.
- It avoids ambiguous note identity when two paths differ only by case.
- It keeps room for platform-specific policies when macOS/Linux support is added.


### ADR-0007: OKF Profile

Status: Accepted.

Decision: Use the GoogleCloudPlatform `knowledge-catalog/okf/SPEC.md` OKF version 0.1 draft as GoMental's authoritative OKF profile.

Key implications:

- A workspace is an OKF Knowledge Bundle: a directory tree of UTF-8 Markdown `.md` files.
- Non-reserved `.md` files are concept documents.
- `index.md` and `log.md` are reserved filenames and are not ordinary concept documents.
- A concept ID is the bundle-relative path with the `.md` suffix removed, normalized with `/` separators.
- Concept documents have YAML frontmatter delimited by `---` and a Markdown body.
- Frontmatter requires non-empty `type`; optional standard fields include `title`, `description`, `resource`, `tags`, and `timestamp`.
- Consumers must tolerate unknown frontmatter keys and broken links.
- OKF hard links are standard Markdown links, including absolute bundle-relative links beginning with `/` and relative links.
- Wiki links and aliases may remain GoMental extensions, but are not OKF v0.1 requirements.
## Phase 0 Findings So Far

- Go is available: `go1.26.5 windows/amd64`.
- Node is available: `v26.3.0`.
- npm is available: `11.16.0`.
- `CGO_ENABLED=0` in the current shell environment.
- Wails CLI was not initially on `PATH`; installed Wails CLI `v2.13.0` to `C:\Users\addy\go\bin\wails.exe`.
- `wails doctor` reports the system is ready for Wails development, with WebView2, Node, and npm installed.
- Disposable Wails React/TypeScript smoke project initialized successfully.
- npm initially blocked `esbuild` install scripts under its allow-scripts policy. Running `npm approve-scripts esbuild` and `npm rebuild esbuild` fixed the disposable frontend dependency install.
- `npm run build` succeeds in the disposable Wails React/TypeScript frontend.
- Disposable Wails `go build ./...` succeeds when using an isolated `GOCACHE` under temp.
- `wails build -s -skipbindings -nopackage` succeeds, proving the Wails Go/resource compilation path works when frontend compilation/embed handoff is skipped.
- Root cause/workaround for Wails build issue found: Wails captures frontend build stdout/stderr via Go `exec.Command`; npm/batch/PowerShell shims can exit with `Access is denied` after a successful Vite build. Configure Wails `frontend:build` as `node node_modules/vite/bin/vite.js build` and run TypeScript typecheck separately. With this configuration, full `wails build -skipbindings` succeeds.
- Wails module latest verified by `go list`: `github.com/wailsapp/wails/v2 v2.13.0`.
- Bleve module latest verified by `go list`: `github.com/blevesearch/bleve/v2 v2.6.0`.
- Bleve disk index spike succeeded: create index, add document, close, reopen, search.
- SQLite driver candidates checked: `modernc.org/sqlite v1.53.0`, `github.com/mattn/go-sqlite3 v1.14.48`, and `github.com/ncruces/go-sqlite3 v0.35.2`.
- SQLite graph spike succeeded with `modernc.org/sqlite v1.53.0` and `CGO_ENABLED=0`: migration, hard-link insert, soft-link insert, close/reopen, outgoing links, backlinks, and recursive neighborhood query.
- Cayley module latest observed as `github.com/cayleygraph/cayley v0.7.7`; Cayley is rejected by ADR-0001.
- Authoritative OKF spec provided by user: GoogleCloudPlatform `knowledge-catalog/okf/SPEC.md`, version 0.1 draft. It defines OKF as a directory of UTF-8 Markdown `.md` concept documents with YAML frontmatter; `type` is required; `index.md` and `log.md` are reserved; standard Markdown links express relationships; broken links must be tolerated.


## Phase 1 Findings So Far

- Adopted Wails' root `main.go` convention because Go embed paths need `frontend/dist` to be reachable from the command package.
- Removed the old console-only placeholder command and `internal/app` placeholder.
- Added Wails app binding in `app.go` with a minimal `Info` DTO for future frontend/API wiring.
- Added React + TypeScript frontend shell with toolbar, notes sidebar, editor pane, and graph/backlinks inspector placeholder.
- Added Wails `frontend:build` as `node node_modules/vite/bin/vite.js build` and `frontend:dev:watcher` as `node node_modules/vite/bin/vite.js`.
- Added `frontend/scripts/npm-install.mjs` and configured Wails `frontend:install` as `node scripts/npm-install.mjs` to avoid npm/batch wrapper `Access is denied` failures under captured Wails output.
- Ran `npm install`, approved/rebuilt `esbuild`, `npm run typecheck`, `go test ./...`, `wails build -skipbindings`, full `wails build` with binding generation, and `wails dev` live-window verification.
- Production executable built successfully at `F:\\dev\\Go\\GoMental\\build\\bin\\GoMental.exe`.
- `wails dev` opened a `GoMental-dev.exe` window titled `GoMental`; the dev check process was stopped after verification.

## Phase 2 Findings So Far

- Added domain note types in `internal/domain`: `NoteID`, `NotePath`, `OKFDocument`, `Note`, `NoteSummary`, `FileVersion`, and `NoteRepository`.
- Added workspace root validation and metadata path handling in `internal/workspace`.
- Implemented OKF concept IDs as workspace-relative `.md` paths without the `.md` suffix, normalized to `/` separators.
- Reject absolute IDs, traversal attempts, root `index.md`/`log.md` concept IDs, and paths escaping the workspace.
- Preserve actual filesystem-relative display paths while applying a Windows-first case-insensitive collision policy key.
- Implemented note scanning for non-reserved `.md` concept documents, excluding `.workspace/`, `.git/`, `node_modules/`, temporary files, hidden dotfiles, and non-Markdown files.
- Implemented `FileNoteRepository` list, read, save, delete, and basic rename behavior using UTF-8 raw OKF content at the repository boundary.
- Implemented recent workspace persistence with a JSON store and bounded most-recent-first ordering.
- Added tests for path normalization, workspace-boundary enforcement, scan exclusions, case-fold policy, CRUD, rename, invalid UTF-8, and recent workspace persistence.
- Verified `go test ./...` and full `wails build` after Phase 2 changes.

## Phase 3 Findings So Far

- Added parsed OKF domain types in `internal/domain`: `OKFMetadata`, `ParsedOKFNote`, `Heading`, `ParsedLink`, `LinkKind`, `LinkStrength`, and structured `DecodeError`.
- Added `internal/okf` codec/parser using `gopkg.in/yaml.v3` for YAML frontmatter.
- Implemented OKF frontmatter decoding for required `type`, optional `title`, `description`, `resource`, `tags`, `timestamp`, and tolerated unknown metadata.
- Implemented OKF encoding back to UTF-8 Markdown with YAML frontmatter and Markdown body.
- Implemented title fallback order: frontmatter `title`, first H1, first heading, concept ID basename.
- Extracted Markdown headings, normalized heading slugs, frontmatter tags, plain text, standard Markdown links, and GoMental wiki-link extension links.
- Standard Markdown links and wiki links are represented as hard links; image links are ignored.
- Added a dedicated resolver that resolves absolute bundle-relative Markdown links, relative Markdown links, wiki links, same-note heading links, and preserves unresolved links with `ResolvedID == nil`.
- Added tests for metadata parsing, unknown field preservation, title fallback, structured decode errors, link extraction, link resolution, unresolved links, and encode/decode behavior.
- Verified `go test ./...`, `npm run typecheck`, `go mod tidy`, and full `wails build` after Phase 3 changes.

## Phase 4 Findings So Far

- Added search domain types in `internal/domain`: `SearchDocument`, `SearchQuery`, `SearchResult`, `SearchIndex`, and `SearchDocumentFromParsed`.
- Added Bleve adapter in `internal/search` using `github.com/blevesearch/bleve/v2 v2.6.0`.
- Search documents include note ID/path, title, body/plain text, headings, tags, aliases, link targets, and modification timestamp.
- Ranking boosts roughly follow `title > aliases > headings > tags > body`.
- Search supports keyword/multi-word queries, prefix matching on title/headings, tag filters, path-prefix filters, limits, highlighting, incremental index, delete, close/reopen, and complete rebuild.
- Rebuild deletes and recreates only the derived index path; it does not touch OKF note files.
- Added tests for title/body ranking, filters, highlight fragments, deletion, close/reopen persistence, and rebuild replacement.

## Phase 5 Findings So Far

- Added graph domain types in `internal/domain`: `NoteLink`, `InferredNoteLink`, `LinkEvidence`, graph nodes/edges, `Graph`, `GraphFilter`, `GraphStore`, and `LinkInferenceService`.
- Added SQLite graph store in `internal/graph` using `modernc.org/sqlite v1.53.0`.
- SQLite graph data is intended for `.workspace/graph/graph.sqlite` through `GraphPath`.
- Implemented schema migration, atomic hard-link outgoing replacement, soft-link replacement, delete-note cleanup, outgoing hard links, backlinks, full graph queries, unresolved nodes, soft-link edges, and in-memory neighborhood expansion over persisted edges.
- Hard links are represented as `links_to`; soft links are represented as `inferred_related_to`.
- Implemented deterministic local soft-link inference with title mention, shared tag, and shared heading evidence, thresholding, top-K limiting, algorithm version, score, evidence, and computed timestamp.
- Added tests for edge replacement, backlinks, delete behavior, unresolved graph nodes, hard/soft full graph contents, path filtering, neighborhood behavior, close/reopen persistence, and soft-link inference scoring/threshold/top-K behavior.
- Verified `go test ./...`, `npm run typecheck`, `go mod tidy`, and full `wails build` after Phase 4 and Phase 5 changes.

## Phase 6 Findings So Far

- Added `internal/indexing` rebuild service for full workspace projection rebuilds.
- Rebuild flow opens a workspace, scans OKF concept documents, parses notes with a bounded worker pool, tolerates per-note parse failures, resolves links against the parsed corpus, rebuilds Bleve search, rebuilds SQLite graph hard links, computes/replaces soft links, and writes rebuild state metadata.
- Progress reporting uses `RebuildProgress` with stages: scanning, parsing, indexing, graph, and complete.
- Derived state locations are `.workspace/search/`, `.workspace/graph/graph.sqlite`, and `.workspace/state/rebuild.json`.
- Full rebuild removes and recreates derived search/graph state so stale deleted-note entries are cleared without touching OKF note files.
- Added tests for temp-directory workspace rebuild, search and graph projection creation, rebuild after deleted note, stale entry removal, cancellation, progress reporting, rebuild-state persistence, and partial parse failure tolerance.
- Verified `go test ./...`, `npm run typecheck`, `go mod tidy`, and full `wails build` after Phase 6 changes.

## Phase 7 Findings So Far

- Added `internal/application` service layer with Wails-facing DTOs and structured `AppError` values.
- Added APIs for opening workspaces, listing notes, reading notes, saving notes, deleting notes, search, full graph, local graph neighborhood, backlinks, rebuild, recent workspaces, UI state load, and UI state save.
- Wails `App` now delegates to the application service and exposes the Phase 7 methods through generated bindings.
- Rebuild closes active derived stores before deleting/recreating `.workspace/search/` and `.workspace/graph/`, avoiding Windows file-lock failures.
- Added Wails runtime events for `workspace:loaded`, `note:updated`, `note:deleted`, `index:progress`, and `graph:updated`.
- Added shutdown handling to close active search and graph stores.
- Added application service tests covering workspace open, rebuild, note list/read/save/delete, search, graph, backlinks, recent workspaces, UI state, events, and structured not-open errors.
- Verified `go test ./...`, `npm run typecheck`, `go mod tidy`, and full `wails build`; Wails bindings were regenerated successfully.

## Phase 8 Findings So Far

- Replaced the static mock React shell with a connected Wails API frontend.
- Added native workspace folder selection through `SelectWorkspaceDirectory` and regenerated Wails bindings.
- The shell now loads app info, recent workspaces, remembered UI state, opened workspace metadata, note summaries, selected note content, and backlinks.
- Notes are grouped into a directory-style tree by normalized OKF concept ID path.
- Added open workspace, rebuild, recent workspace open, note selection, progress display, error strip, and workspace fact panel.
- Persisted `lastWorkspace` and `lastNote` through the Phase 7 UI state API.
- Subscribed to Wails events for `index:progress`, `note:updated`, and `note:deleted`.
- Added frontend styles for tree groups, recent workspaces, empty state, status text, errors, and facts.
- Verified `npm run typecheck`, `go test ./...`, full `wails build`, and a short `wails dev` smoke check that opened `GoMental-dev.exe` with window title `GoMental`.
## Phase 9 Findings So Far

- Added CodeMirror 6 dependencies for source-mode OKF/Markdown editing.
- Added `frontend/src/CodeMirrorEditor.tsx` with Markdown syntax highlighting, line numbers, active-line styling, undo/redo history, search panel keybindings, line wrapping, and editor theming.
- Added note-title/ID completion inside `[[wiki links]]` and tag completion after `#` using loaded workspace note summaries.
- Added Ctrl/Cmd-click navigation for GoMental wiki links, normalizing `/absolute`, `.md`, and Windows separator variants to note IDs.
- Replaced the read-only note `<pre>` with editable draft state, dirty/saving/saved/conflict indicators, explicit Save button, and `Mod-S` save shortcut.
- Added sidebar new-note creation with title and note ID fields, automatic slug generation, duplicate-ID checks against loaded notes, starter OKF frontmatter/body generation, immediate selection of the new note, and workspace note-count refresh.
- Added debounced autosave after edits; saves flow through the existing Wails `SaveNote` API so disk, search, and graph projections are updated by the application service.
- Added a lightweight frontend external-modification check before save by re-reading the selected note and comparing `modifiedAt` plus the original saved content. Backend-level compare-and-swap save protection is still a future hardening option.
- Verified `npm run typecheck`, `go test ./...`, full `wails build -v 1 -nocolour`, and a `wails dev` smoke check that compiled the dev app and initialized WebView2 with the `GoMental` development window.
## Phase 10 Findings So Far

- Added a search panel to the left workspace sidebar above the note tree.
- Wired the panel to the existing Wails `Search` API with debounced requests and request-ID based stale result suppression.
- Added search text, tag filters, path-prefix filter normalization, selectable result limits, and incremental `More` loading.
- Rendered ranked search results with title, note ID, and highlighted fragments while splitting backend `<mark>` fragments into safe React text/mark nodes instead of injecting raw HTML.
- Added click-to-open behavior so selecting a search result opens that note in the editor.
- Kept empty, searching, ready, and error search states visible in the sidebar.
- Verified `npm run typecheck`, `go test ./...`, and full `wails build -v 1 -nocolour`.
## Phase 11 Findings So Far

- Added `graphology` and `sigma` frontend dependencies.
- Added `frontend/src/GraphPanel.tsx` as a dedicated Graphology/Sigma graph renderer.
- Wired local graph mode to the existing Wails `Neighborhood` API with selectable depth.
- Wired global graph mode to the existing Wails `FullGraph` API with path-prefix, tag, soft-link, and unresolved-node filters.
- Added deterministic temporary circular/ring layout so graphs are visible before Phase 12 layout persistence.
- Added Sigma click-to-open note behavior, hover-neighborhood emphasis, node search filtering, refresh, and camera reset controls.
- Replaced the right-pane placeholder area with the graph panel while keeping workspace facts and backlinks visible below it.
- Verified `npm run typecheck`, `go test ./...`, full `wails build -v 1 -nocolour`, and a `wails dev` smoke check that compiled the dev app and initialized WebView2.
## Phase 12 Findings So Far

- Added Wails/application APIs for `LoadGraphLayout` and `SaveGraphLayout`.
- Added `LayoutCoordinatesDTO` and `LayoutSnapshotDTO` and persisted graph layout snapshots to `.workspace/layout/graph-layout.json`.
- Added application-service test coverage for graph layout save/load and layout file creation.
- Added `graphology-layout-forceatlas2` frontend dependency.
- Added `frontend/src/graphLayoutWorker.ts`, a module Web Worker that runs bounded ForceAtlas2 layout off the UI thread.
- Updated `GraphPanel` to load saved coordinates before rendering, seed new nodes with generated positions, run ForceAtlas2 in the worker, apply stabilized positions to Sigma, and save merged coordinates back to the workspace layout snapshot.
- Kept Phase 11 interactions active: local/global graph modes, filters, click-to-open, hover neighborhood emphasis, refresh, and camera reset.
- Verified `npm run typecheck`, `go test ./...`, full `wails build -v 1 -nocolour`, and a `wails dev` smoke check that compiled the dev app, initialized WebView2, and optimized the worker dependency.
## Phase 13 Findings So Far

- Added `internal/platform.WorkspaceWatcher`, a polling watcher that uses the same OKF `.md` scanning and exclusion policy as the workspace repository.
- Watcher snapshots compare note IDs, modification timestamps, and file sizes, coalescing create, modify, rename, and delete effects into changed/deleted note IDs while ignoring temporary and metadata files through the existing scanner.
- Application service now starts a watcher when a workspace opens and stops it on close, rebuild, or workspace switch.
- Watch events flow through a bounded channel and a debounce window before projection work runs, avoiding unbounded goroutines during bursty editor writes.
- Incremental projection updates delete removed notes from Bleve and SQLite, index changed/created notes, re-resolve hard links for the remaining corpus, refresh soft links, and preserve unresolved links after deletes.
- Frontend Wails event handling now refreshes the note list, selected rendered note when not editing, backlinks, and graph panel after external note and graph updates.
- Added focused tests for watcher snapshot diffs and application-level external create/modify/delete projection updates.
- Verified `npm run typecheck`, `go test ./...`, and full `wails build -v 1 -nocolour`.
## Phase 14 Findings So Far

- Added stable AppError code constants for workspace accessibility, OKF decode failure, corrupt search index, corrupt graph store, external conflict, broken rename, missing projection state, stale projection state, and repair failure.
- Workspace opening now checks derived projection health before exposing the workspace: missing search/graph/state, stale rebuild state, newer notes than the last rebuild, and adapter open failures all route through repair.
- Derived-state repair uses the existing `indexing.Rebuilder`, which removes and rebuilds only `.workspace/search/` and `.workspace/graph/` from authoritative OKF notes.
- Active stores and watchers are stopped before repair to avoid Windows file locks.
- Recovery attempts are logged to `.workspace/logs/GoMental.log` with reason, status, and error detail for local diagnosis.
- The frontend error strip now offers a direct `Rebuild projections` action when a workspace is open.
- Added focused application tests covering missing derived state, corrupt Bleve search projection, corrupt SQLite graph projection, recovery logging, and preserving OKF note content during repair.
- Verified `npm run typecheck`, `go test ./...`, and full `wails build -v 1 -nocolour`.
## Open Issues

| ID | Phase | Issue | Status | Owner | Notes |
| --- | --- | --- | --- | --- | --- |
| OI-0001 | 0 | Verify OKF specification/profile, file extension policy, metadata/body model, and link model. | Closed | User/Codex | Authoritative source provided: https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md. Profile: OKF v0.1 draft. |
| OI-0002 | 0 | Select SQLite Go driver and migration strategy. | Closed | Codex | Driver selected: `modernc.org/sqlite v1.53.0`. Migration strategy: SQL schema migrations under adapter ownership; exact migration package/tooling still to choose during implementation. |
| OI-0003 | 0 | Run SQLite graph-store spike: migrate schema, insert hard/soft links, close, reopen, outgoing links, backlinks, and neighborhood query. | Closed | Codex | Disposable spike passed under `%TEMP%\GoMental-phase0-sqlite-spike`. |
| OI-0004 | 0 | Resolve Wails production build `Access is denied` after frontend compilation. | Closed | Codex | Fixed by avoiding npm/batch wrappers in Wails frontend build: use `node node_modules/vite/bin/vite.js build`; keep typecheck as a separate script/command. Full Wails smoke build now succeeds. |
| OI-0005 | 0 | Decide final Go module path and command layout. | Closed | Codex | Keep module `GoMental` and command `cmd/GoMental` unless Wails scaffolding forces a later adapter-level change. |
| OI-0006 | 0 | Confirm metadata directory constant. | Closed | Codex | Use `.workspace/`. |
| OI-0007 | 0 | Document note ID case-sensitivity policy. | Closed | Codex | Windows policy accepted: preserve casing for display, normalize separators to `/`, detect collisions case-insensitively. |
| OI-0008 | 0 | Choose SQLite migration package/tooling. | Open | Unassigned | Can be a lightweight internal migration runner for first version; no external tool selected yet. |
| OI-0009 | 1 | Verify `wails dev` opens the app window with the Phase 1 shell. | Closed | Codex | Verified on 2026-07-14: Vite started, dev app compiled, and `GoMental-dev.exe` opened with window title `GoMental`; process stopped after check. |

## Handoff Notes

- Do not continue Cayley investigation unless the SQLite decision is explicitly reversed.
- Keep graph persistence behind `GraphStore`; do not expose SQL rows or query details to application services or frontend DTOs.
- Use `modernc.org/sqlite` first. Keep `mattn/go-sqlite3` as a fallback only if modernc creates unacceptable runtime or packaging issues.
- Soft links should be stored as derived graph data, not written into OKF notes unless the user promotes them to hard links.
- For Wails build troubleshooting, start from the disposable smoke project in `%TEMP%\GoMental-phase0-wails-smoke\GoMental-smoke` if it still exists.
- For Bleve verification, the disposable spike used `%TEMP%\GoMental-phase0-storage-spikes\bleve` and wrote an index under `%TEMP%\GoMental-phase0-bleve-index`.
- For SQLite verification, the disposable spike used `%TEMP%\GoMental-phase0-sqlite-spike` and wrote `%TEMP%\GoMental-phase0-graph.sqlite`.

## Next Actions

1. Start Phase 15: add benchmark workspace generation and baseline performance measurements.
2. Choose SQLite migration tooling or implement a tiny internal migration runner before graph schema evolution becomes necessary.
3. Consider backend-level save conflict protection after the frontend conflict warning has had real-workspace exercise.






















