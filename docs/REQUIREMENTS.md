# GoMental Requirements

This document extracts implementation requirements from the product prompt and project plan. It is organized so individual requirements can be converted into issues, tests, and acceptance criteria.

## 1. Product Scope

GoMental shall be a local-first desktop knowledge-management app for Google's **Open Knowledge Format (OKF)** notes and knowledge graphs.

The first implementation shall deliver a vertical slice where a user can open a local OKF workspace, browse notes, edit notes, search notes, view a persistent graph projection with hard and optional soft links, and restart the app without losing notes or derived state.

## 2. Product Constraints

### REQ-PC-001 Local-only operation

The application shall run entirely on the user's machine.

Acceptance criteria:

- The app can start and operate without internet connectivity.
- No cloud database, hosted search engine, hosted graph service, or external network service is required.

### REQ-PC-002 Desktop distribution

The application shall be distributed as a desktop application.

Acceptance criteria:

- Frontend assets are built and embedded into the desktop app.
- The user does not need to start a backend process manually.
- The app does not open a visible browser window for normal operation.

### REQ-PC-003 No Electron

The application shall avoid Electron and its bundled Chromium runtime.

Acceptance criteria:

- Wails is used for the desktop shell.

### REQ-PC-004 Windows-first platform target

The initial target platform shall be Windows.

Acceptance criteria:

- Platform-specific code is isolated so macOS and Linux support can be added later.

## 3. Architecture Requirements

### REQ-ARCH-001 Required stack

The application shall use:

- Wails for the desktop shell.
- Go for the backend.
- React and TypeScript for the frontend.
- CodeMirror 6 for OKF note editing, including OKF/Markdown-compatible body editing where the selected OKF profile uses Markdown-like text content.
- Bleve for full-text search.
- SQLite, accessed through `GraphStore` with `modernc.org/sqlite`, for local graph persistence.
- Graphology for browser-side graph data.
- Sigma.js for WebGL graph rendering.
- Graphology ForceAtlas2 for graph layout.

### REQ-ARCH-002 Wails communication

The frontend shall communicate with Go through Wails-generated bindings.

Acceptance criteria:

- No separately deployed HTTP service is required.
- No network listener is exposed unless a later explicit feature requires one.

### REQ-ARCH-003 Domain service boundaries

The backend shall be organized around explicit application/domain services.

Acceptance criteria:

- UI code does not call Bleve, SQLite, or filesystem adapters directly.
- Wails DTOs are mapped from domain models.
- Frontend DTOs do not become the domain model.

### REQ-ARCH-004 Replaceable adapters

Bleve and SQLite graph persistence shall sit behind interfaces.

Acceptance criteria:

- Search code depends on `SearchIndex`, not Bleve APIs outside the adapter.
- Graph code depends on `GraphStore`, not SQLite APIs or schema details outside the adapter.

## 4. Workspace Requirements

### REQ-OKF-001 Standard note format

The application shall use Google's **Open Knowledge Format (OKF)** as the standard persisted note format.

Acceptance criteria:

- The selected OKF version/profile is documented as GoogleCloudPlatform/knowledge-catalog OKF SPEC.md, version 0.1 draft.
- Core note datatypes use OKF naming where appropriate, including OKFDocument, OKFNote, OKFMetadata, ParsedOKFNote, and OKF-aware link/tag/metadata structures.
- GoMental-specific derived state is not written into authoritative OKF note content unless OKF explicitly defines the field as user-authored note data.

### REQ-OKF-002 OKF codec

The application shall include an OKF codec/parser at the repository boundary.

Acceptance criteria:

- UTF-8 Markdown `.md` OKF concept documents with YAML frontmatter can be decoded into domain objects.
- Edited notes can be serialized back to OKF while preserving user-authored fields needed for round-tripping.
- Invalid OKF, including non-UTF-8 content, missing/invalid frontmatter, or missing required `type` for concept documents, produces structured decode errors.

### REQ-WS-001 Workspace source of truth

Each workspace shall be an ordinary local OKF bundle directory containing Markdown `.md` concept documents.

Acceptance criteria:

- OKF note files are readable and editable outside GoMental by OKF-compatible tools.
- Authoritative OKF note content is not stored only in a proprietary database.

### REQ-WS-002 Note identity

The first version shall use OKF concept IDs as stable note identifiers: workspace-relative `.md` paths with the `.md` suffix removed.

Acceptance criteria:

- Path separators are normalized to `/`.
- Paths escaping the workspace root are rejected.
- Actual filename casing is preserved for display.
- Case-sensitivity policy is documented.

### REQ-WS-003 Metadata directory

Application-owned derived data shall live under a workspace-local metadata directory, initially `.workspace/`.

Acceptance criteria:

- `.workspace/` is excluded from note scanning.
- Search, graph, layout, and state data can be stored under that directory.

### REQ-WS-004 Ignored files

The workspace scanner shall ignore metadata directories, temporary editor files, and hidden system files where appropriate.

### REQ-WS-005 Workspace lifecycle

The application shall let the user open a workspace directory and remember recent workspaces.

Acceptance criteria:

- Recent workspaces are available after restart.
- The last opened note can be restored when possible.

## 5. OKF Note Format Requirements

### REQ-OKF-003 OKF parsing

The OKF parser shall extract, according to OKF v0.1 draft:

- YAML frontmatter delimited by `---`.
- Markdown body headings.
- Standard Markdown links, including absolute bundle-relative links beginning with `/` and relative links.
- Wiki-style links such as `[[Target Note]]` as a GoMental extension, not an OKF v0.1 requirement.
- Wiki aliases such as `[[Target Note|Display text]]` as a GoMental extension, not an OKF v0.1 requirement.
- Tags from the OKF `tags` frontmatter list; inline hashtag extraction may be a GoMental extension.
- Embedded references if cleanly supportable.

### REQ-OKF-004 Parsed OKF note output

For every concept document, the parser shall derive `ParsedOKFNote`, containing concept ID, OKF metadata (`type`, optional `title`, `description`, `resource`, `tags`, `timestamp`, and unknown keys), Markdown body/content, plain text, headings, citations, links, and file modification timestamp.

### REQ-OKF-005 Link resolution separation

Link resolution shall be handled by a dedicated resolver, not buried inside parsing code.

Acceptance criteria:

- Parser tests can run without filesystem link-resolution behavior.
- Resolver tests cover target matching and unresolved links.

### REQ-OKF-006 Unresolved links

Unresolved links shall be represented explicitly.

Acceptance criteria:

- A link to a missing note appears in graph/query data as unresolved rather than silently disappearing.

## 6. Search Requirements

### REQ-SRCH-001 Local persistent index

Search shall use Bleve as an in-process, disk-persistent full-text index.

Acceptance criteria:

- Index data is stored under workspace derived state.
- The index can be reopened after app restart.

### REQ-SRCH-002 Indexed fields

Search shall index fields separately:

- Note ID/path.
- Title.
- Plain-text body.
- Headings.
- Tags.
- Aliases.
- Explicit hard-link targets.
- Inferred soft-link targets and evidence summaries where useful.
- Modification timestamp.

### REQ-SRCH-003 Ranking preference

Initial search ranking shall prefer fields roughly in this order: title, aliases, headings, tags, body.

### REQ-SRCH-004 Search behavior

Search shall support:

- Keyword search.
- Multi-word queries.
- Phrase queries.
- Prefix matching suitable for search-as-you-type.
- Result ranking.
- Highlighted matching fragments.
- Filtering by tag.
- Filtering by path or directory.
- Result limits and pagination or cursoring.

### REQ-SRCH-005 Incremental and rebuild operations

Search shall support incremental updates after note changes and a complete index rebuild. Search-derived similarity may be used as one local signal for soft-link inference.

### REQ-SRCH-006 UI cancellation

Search shall run asynchronously from the UI perspective and support cancellation of stale requests.

## 7. Indexing Consistency Requirements

### REQ-IDX-001 Markdown authority

OKF note files shall be authoritative. Search and graph stores are rebuildable projections.

### REQ-IDX-002 Indexing pipeline

The application shall maintain an indexing pipeline:

```text
filesystem event -> debounce -> read OKF file -> decode/parse OKF -> update search index -> update graph projection -> notify UI
```

### REQ-IDX-003 File change coverage

The indexing pipeline shall handle:

- File creation.
- Modification.
- Rename.
- Deletion.
- Changes made by external editors.
- Bursts of filesystem events.
- Temporary files.
- Partial writes.
- Startup after unclean shutdown.

### REQ-IDX-004 Stale derived-state detection

The application shall persist enough metadata to detect stale index entries, without requiring the index to recover note content.

### REQ-IDX-005 Rebuild command

The application shall provide a command that discards and rebuilds all derived indexes from the workspace.

## 8. Link Model And Graph Persistence Requirements

### REQ-LINK-001 Hard links

The application shall treat explicit user-authored OKF links as hard links.

Acceptance criteria:

- Standard Markdown OKF links produce hard-link domain records. Wiki links, aliases, and supported embedded references may produce hard-link records only as GoMental extensions.
- Hard links are stored in graph projections as explicit `links_to` relationships.
- Hard links participate in backlinks and broken-link reporting.

### REQ-LINK-002 Soft links

The application shall support soft links: inferred note-to-note relationships that are not explicitly authored in OKF files.

Acceptance criteria:

- Soft links are stored as derived projection data, not authoritative note content.
- Soft links include source, target, score, evidence, algorithm version, and computed timestamp.
- Soft links can be hidden, shown, filtered by score, and rebuilt.
- A soft link becomes a hard link only if the user explicitly promotes it by adding an OKF link to the note.

### REQ-LINK-003 Local link inference mechanism

The first soft-link inference mechanism shall be local, explainable, and deterministic enough for repeatable tests.

The initial scorer should combine these signals:

- Title or alias mentions in another note's body/content.
- Shared normalized tags, with rarer tags weighted more strongly.
- Shared headings, aliases, and extracted keyphrases.
- Bleve/BM25 similarity over title, headings, tags, aliases, and plain text.
- Hard-link graph topology such as common neighbors and two-hop paths.

Acceptance criteria:

- Inference requires no hosted AI service and no network connectivity.
- Each inferred link has human-readable evidence.
- The app keeps only top-K soft links per note above a configurable threshold.
- The inference algorithm version is persisted so stale soft links can be recomputed.

### REQ-LINK-004 Future local semantic inference

The architecture may preserve extension points for local embeddings or vector similarity, but hosted embeddings and ANN vector search remain outside the first milestone.

## 9. Graph Persistence Requirements

### REQ-GRAPH-001 SQLite graph-store verification

Before implementation, the project shall verify `modernc.org/sqlite`, migration approach, transaction behavior, and local database file layout for graph persistence.

### REQ-GRAPH-002 Derived graph projection

The graph shall be a derived projection of OKF links, tags, and references, not a source of truth.

### REQ-GRAPH-003 Node types

The graph shall represent at least:

- Existing notes.
- Unresolved note references.
- Tags, if tags are included in the graph.

### REQ-GRAPH-004 Edge types

The graph shall represent at least:

- `links_to` for hard links.
- `inferred_related_to` for soft links.
- `tagged_with`, if tags are included.
- `semantic_similarity` reserved for later local-only semantic inference.

### REQ-GRAPH-005 Edge metadata

Hard note-to-note edges shall preserve source, target, resolved ID if present, display text, and heading where available. Soft note-to-note edges shall preserve source, target, score, evidence, algorithm version, and computed timestamp.

### REQ-GRAPH-006 Outgoing edge replacement

When a note changes, the graph store shall replace the note's outgoing hard-link edge set transactionally or as close to atomically as the backend permits. Soft-link sets shall be recomputed or marked stale according to the inference pipeline.

### REQ-GRAPH-007 Required graph queries

The graph store shall support:

- Direct outgoing hard links.
- Inferred outgoing and incoming soft links.
- Direct backlinks.
- Neighborhood to configurable depth with hard/soft link filtering.
- Orphan notes.
- Broken or unresolved links.
- Full graph with filters, including hard-only, soft-only, and combined views.
- Nodes matching tags or path prefixes.

## 10. Graph Rendering Requirements

### REQ-GUI-GRAPH-001 Rendering projection

The backend shall return graph DTOs tailored to rendering and shall not send graph database internals to Sigma.

### REQ-GUI-GRAPH-002 Graphology and Sigma

The frontend shall use Graphology as the graph data model and Sigma.js as the WebGL renderer.

### REQ-GUI-GRAPH-003 No per-node DOM rendering

The graph shall not render a DOM element for every node.

### REQ-GUI-GRAPH-004 Global graph interactions

The global graph shall support:

- Pan and zoom.
- Hover neighborhood emphasis.
- Click node to open note.
- Double-click or explicit focus around a node.
- Node search by title or path.
- Tag filters.
- Path-prefix filters.
- Hide unresolved nodes.
- Hide orphan nodes.
- Show or hide soft links.
- Filter soft links by minimum score.
- Neighborhood depth control.
- Node-size strategy control.
- Reset camera.
- Re-run layout.

### REQ-GUI-GRAPH-005 Local graph interactions

The local graph shall support:

- Incoming links.
- Outgoing links.
- Both directions.
- Depth of one or more hops.
- Optional unresolved links.
- Optional tag nodes.
- Optional soft links above a score threshold.

### REQ-GUI-GRAPH-006 Semantic zoom

The graph shall show labels selectively at distant zoom levels and more labels as the user zooms in, prioritizing selected, hovered, highly connected, and searched nodes.

## 11. Graph Layout Requirements

### REQ-LAYOUT-001 Worker-based layout

ForceAtlas2 layout shall run in a Web Worker.

### REQ-LAYOUT-002 Bounded simulation

The force simulation shall not run indefinitely.

Acceptance criteria:

- Layout stops after stabilization or a fixed iteration budget.

### REQ-LAYOUT-003 Layout lifecycle

The layout shall:

1. Load stored coordinates when available.
2. Assign initial positions to new nodes.
3. Run ForceAtlas2 in a worker.
4. Stop after stabilization or a fixed iteration budget.
5. Persist coordinates.
6. Re-run only after material graph changes or explicit user action.

### REQ-LAYOUT-004 Coordinate persistence

Coordinates shall be stored separately from graph relationships.

## 12. OKF Note Editor Requirements

### REQ-EDIT-001 CodeMirror editor

The initial editor shall use CodeMirror 6 in source-mode OKF note editing, with syntax support appropriate to the selected OKF profile and its body/content representation.

### REQ-EDIT-002 Editor features

The editor shall support:

- OKF/Markdown-compatible syntax highlighting.
- Optional line numbers.
- Undo and redo.
- Find and replace.
- Wiki-link autocompletion.
- Note-title autocompletion.
- Tag autocompletion.
- Ctrl/Cmd-click navigation for links.
- Dirty-state indication.
- Autosave with debounce.
- Explicit save shortcut.
- Safe handling of external file modifications.

### REQ-EDIT-003 No first-milestone WYSIWYG

The first implementation shall not attempt a full live-preview or WYSIWYG editor.

## 13. Desktop UX Requirements

### REQ-UX-001 Initial layout

The initial layout shall include:

```text
Toolbar / command search
File tree and search | OKF note editor | Backlinks or local graph
```

### REQ-UX-002 Global graph location

The global graph may open as a main-view tab rather than a permanently visible pane.

### REQ-UX-003 Application state restoration

The application shall restore:

- Recent workspaces.
- Most recently opened note.
- Pane sizes.
- Basic UI state.

### REQ-UX-004 Keyboard shortcuts

The application shall support application-level keyboard shortcuts.

## 14. Concurrency Requirements

### REQ-CONC-001 Context propagation

Go contexts shall be used consistently for potentially slow operations.

### REQ-CONC-002 Non-blocking UI calls

Potentially slow operations shall not block Wails UI calls indefinitely.

Operations include:

- Workspace scans.
- Index rebuilds.
- Search.
- Graph extraction.
- Full graph loading.
- File parsing.

### REQ-CONC-003 Bounded concurrency

The application shall use bounded worker pools where concurrency is useful.

Acceptance criteria:

- The app does not spawn an unbounded goroutine per file.

### REQ-CONC-004 Cancellation

The app shall support cancellation for:

- Search requests.
- Workspace scans.
- Rebuild operations.
- Graph queries.

## 15. Error Handling Requirements

### REQ-ERR-001 Structured errors

The backend shall return structured application errors rather than raw storage-library errors.

Suggested shape:

```go
type AppError struct {
    Code    string
    Message string
    Detail  string
}
```

### REQ-ERR-002 Explicit error cases

The application shall handle:

- Workspace inaccessible.
- OKF note file cannot be decoded or validated.
- Search index corrupt.
- Graph store corrupt.
- File changed externally during editing.
- Note rename breaks links.
- Derived state missing or stale.

### REQ-ERR-003 Safe recovery

For corrupt or missing derived state, the application shall offer to rebuild it without risking OKF note files.

## 16. Testing Requirements

### REQ-TEST-001 Required tests

The project shall include tests for:

- Path normalization and workspace-boundary enforcement.
- OKF link extraction.
- Wiki-link parsing.
- Link resolution.
- Unresolved-link handling.
- Rename and delete behavior.
- Search indexing and deletion.
- Search ranking of title versus body matches.
- Search-index rebuild.
- Graph edge replacement.
- Soft-link scoring, evidence generation, thresholding, and rebuild behavior.
- Backlink queries.
- Filesystem event coalescing.
- Stale event ordering.
- Persistence and reload of layout coordinates.

### REQ-TEST-002 Temporary directories

Integration tests shall use temporary directories and shall not depend on the developer's real workspace.

### REQ-TEST-003 Backend tests before UI depth

Backend integration tests for workspace loading, search indexing, and graph updates shall be established before substantial UI code.

## 17. Benchmark Requirements

### REQ-BENCH-001 Generated benchmark workspace

The project shall include a generated benchmark workspace containing at least:

- 10,000 notes.
- Sparse links.
- Tags.
- Orphans.
- Broken links.
- A smaller set of highly connected notes.

### REQ-BENCH-002 Benchmark measurements

The benchmark workspace shall be used to measure:

- Initial scan time.
- Index build time.
- Search latency.
- Incremental update latency.
- Graph query latency.
- Soft-link inference latency and derived storage size.
- Graph payload size.
- Frontend rendering performance.

## 18. First Vertical Slice Requirements

The first vertical slice shall support this end-to-end workflow:

1. Start the desktop application.
2. Select a directory containing OKF notes.
3. Scan and parse the notes.
4. Build a Bleve index.
5. Build the graph projection.
6. Display the note tree.
7. Open and edit a note in CodeMirror.
8. Save the note to disk.
9. Update search and graph state incrementally.
10. Search notes and open a result.
11. Display a Sigma global graph.
12. Click a graph node and open its note.
13. Restart the application and confirm that notes, indexes, graph data, and layout remain available.

## 19. Delivery Sequence Requirements

Implementation shall proceed in this order unless a later design decision explicitly changes it:

1. Repository and Wails scaffolding.
2. Workspace path model and note repository.
3. OKF codec/parser and link extraction.
4. Bleve search adapter.
5. GraphStore abstraction, SQLite adapter, and hard/soft link model.
6. Initial workspace scan and rebuild pipeline.
7. Wails application APIs.
8. React shell and file tree.
9. CodeMirror editor.
10. Search UI.
11. Graphology and Sigma integration.
12. ForceAtlas2 worker and coordinate persistence.
13. Filesystem watching and incremental updates.
14. Error recovery and rebuild flows.
15. Benchmarks and performance tuning.

## 20. First Milestone Non-Goals

The first milestone shall not implement:

- Cloud synchronization.
- User accounts.
- Collaboration.
- Mobile support.
- Hosted AI services.
- External graph or search servers.
- Hosted semantic embeddings.
- ANN vector search.
- RAG answer generation.
- Plugin execution.
- WYSIWYG editing.
- End-to-end encryption.
- Git synchronization.

Extension points for local embeddings and vector search may be preserved. Deterministic local soft-link inference may be included in the first vertical slice, but embeddings and ANN vector search shall not be required.

## 21. Server, Multi-User & Agent Requirements (Phases 16–24)

These additive requirements cover server mode; the desktop app remains single-user and unchanged (G1).

### REQ-SRV-001 Additive server mode
`gomental serve` shall serve the wiki over HTTP without altering desktop behavior. All front doors (Wails, HTTP, MCP) shall be thin adapters over one `application.Service` per workspace.

### REQ-SRV-002 HTTP API + event stream
The server shall expose the full Service surface as JSON REST plus a Server-Sent Events stream (`/api/events`), reusing the application DTOs, and shall serve the embedded browser SPA with client-route fallback.

### REQ-SRV-003 Optimistic concurrency
Note reads shall return an opaque version token; saves shall reject a stale write with `edit.external_conflict` / HTTP 412 (honoring `If-Match`). Concurrent saves to the same note shall serialize; the save path shall not scale with corpus size.

### REQ-SRV-004 Identity, authorization & audit
The server shall support pluggable authentication with coarse roles (viewer/editor/admin) and shall record every write to an append-only audit log under `.workspace/`. The default posture is trust-all (LAN); untrusted exposure requires enabling a real authenticator or an authenticating reverse proxy.

### REQ-SRV-005 Agent API
Agents shall search/read/create/edit notes over the authenticated REST API using revocable API keys tied to a role, with idempotent create modes and a published OpenAPI spec, participating in optimistic concurrency via version tokens. Per-key rate limiting shall guard against runaway agents.

### REQ-SRV-006 MCP server
The server shall expose MCP tools (search/read/list/create/edit/backlinks/neighborhood) over stdio (`gomental mcp`) reusing the in-process Service.

### REQ-SRV-007 Hardening & ops
The server shall support TLS, CORS allow-listing, request-size limits, SSRF protection for URL import, audit-log rotation, health/readiness/metrics endpoints, and file/env/flag configuration; a container image and systemd unit shall be provided. The workspace directory (including `.workspace/`) is the backup unit.

### REQ-SRV-008 Single-process ownership
Exactly one server process shall own a workspace (Bleve exclusive lock; single SQLite graph file). Horizontal write scaling over one workspace is out of scope.












