# GoMental Project Plan

GoMental is a local-first, Obsidian-like desktop knowledge management app. The first release should prove the full note lifecycle: open a local OKF workspace, parse notes, build search and graph projections, edit notes, update indexes incrementally, render the graph, and restart with durable local state.

## Product Guardrails

- Run entirely on the user's machine with no required network connectivity or hosted services.
- Use OKF note files in a user-selected workspace as the source of truth.
- Use Google's **Open Knowledge Format (OKF)** as the standard note format for persisted notes and related note datatypes.
- Distinguish hard links, which are explicit user-authored OKF links, from soft links, which are locally inferred, scored, rebuildable suggestions.
- Store only derived application data under a workspace-local metadata directory, initially `.workspace/`.
- Avoid Electron and bundled Chromium. Use Wails for the desktop shell.
- Keep frontend assets bundled into the desktop app.
- Target Windows first while isolating platform-specific code for future macOS and Linux support.
- Optimize for tens of thousands of notes with sparse relationships.

## Target Architecture

```text
Desktop shell:       Wails
Backend:             Go
Frontend:            React + TypeScript
OKF note editor:     CodeMirror 6
Full-text search:    Bleve
Graph persistence:   SQLite through a GraphStore adapter (`modernc.org/sqlite`)
Graph UI model:      Graphology
Graph rendering:     Sigma.js
Graph layout:        Graphology ForceAtlas2 in a Web Worker
Note format:         Google's **Open Knowledge Format (OKF) v0.1 draft**
Source of truth:     OKF note files on disk
```

Suggested repository layout:

```text
/cmd/app
/internal/application
/internal/domain
/internal/workspace
/internal/okf
/internal/search
/internal/graph
/internal/indexing
/internal/persistence
/internal/platform
/frontend
/docs
```

The backend should expose application services to Wails rather than leaking storage libraries, filesystem mechanics, or Wails DTOs into the domain model.

## Key Domain Boundaries

Define interfaces at the application boundary and keep adapters behind them.

```go
type NoteRepository interface {
    List(ctx context.Context) ([]NoteSummary, error)
    Read(ctx context.Context, id NoteID) (Note, error)
    Save(ctx context.Context, note Note) error
    Delete(ctx context.Context, id NoteID) error
}

type SearchIndex interface {
    Index(ctx context.Context, document SearchDocument) error
    Delete(ctx context.Context, id NoteID) error
    Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
    Rebuild(ctx context.Context, documents iter.Seq2[SearchDocument, error]) error
}

type GraphStore interface {
    ReplaceOutgoingLinks(ctx context.Context, source NoteID, links []NoteLink) error
    DeleteNote(ctx context.Context, id NoteID) error
    OutgoingLinks(ctx context.Context, id NoteID) ([]NoteLink, error)
    Backlinks(ctx context.Context, id NoteID) ([]NoteLink, error)
    Neighborhood(ctx context.Context, id NoteID, depth int) (Graph, error)
    FullGraph(ctx context.Context, filter GraphFilter) (Graph, error)
}

type LinkInferenceService interface {
    InferLinks(ctx context.Context, note ParsedOKFNote, corpus NoteCorpus) ([]InferredNoteLink, error)
    Explain(ctx context.Context, source NoteID, target NoteID) (LinkExplanation, error)
}
```

Use explicit DTO mapping for Wails responses. Frontend DTOs should not become the domain model.

## Phase 0: Architecture Verification

Goal: Remove dependency uncertainty before committing to substantial implementation.

Tasks:

- Apply Google's **Open Knowledge Format (OKF) v0.1 draft**: UTF-8 Markdown `.md` concept documents, YAML frontmatter, required `type`, optional `title`/`description`/`resource`/`tags`/`timestamp`, reserved `index.md` and `log.md`, concept IDs as bundle-relative paths without `.md`, and standard Markdown cross-links.
- Verify the current Wails version, project scaffolding expectations, asset embedding behavior, and Windows build workflow.
- Verify the selected SQLite Go driver (`modernc.org/sqlite`), migration strategy, transaction behavior, and local file layout for graph persistence.
- Confirm Bleve version and index storage backend.
- Decide the Go module path and final command path. The current placeholder is `GoMental` with `cmd/GoMental`.
- Decide the workspace metadata directory constant, initially `.workspace/`.
- Document the note ID case-sensitivity policy. Recommended first policy for Windows: preserve casing for display, normalize separators to `/`, and treat IDs case-insensitively for collision detection.

Exit criteria:

- OKF v0.1 draft profile, `.md` file extension policy, reserved filenames, concept ID policy, frontmatter model, body model, and link model are recorded.
- Dependency choices are recorded.
- A Wails app can build locally on Windows.
- SQLite graph storage can be opened, migrated, written, closed, reopened, and queried for outgoing links, backlinks, and neighborhoods in a minimal spike.
- Bleve index can be created under `.workspace/search/` and reopened.

## Phase 1: Repository And Wails Scaffolding

Goal: Replace the placeholder console app with the real desktop application skeleton.

Tasks:

- Add Wails project structure with Go backend and React + TypeScript frontend.
- Keep generated frontend assets embedded in the app binary.
- Establish command entrypoint under `cmd/app` or align the existing `cmd/GoMental` with Wails conventions.
- Add baseline frontend shell with app frame, toolbar, sidebar, editor pane, and right pane placeholder.
- Add build, dev, test, typecheck, and lint commands to `README.md`.
- Add CI-ready commands even if CI is not configured yet.
- Configure Wails `frontend:build` to call Vite directly (`node node_modules/vite/bin/vite.js build`) on Windows; run TypeScript typechecking as a separate command to avoid npm/batch captured-stdio `Access is denied` failures observed in Phase 0.

Exit criteria:

- `wails dev` opens the app window.
- A production build embeds frontend assets.
- No browser or manually managed local backend is required.

## Phase 2: Workspace Path Model And Note Repository

Goal: Safely model an OKF workspace and expose file-backed notes.

Tasks:

- Implement workspace root selection and validation.
- Implement note IDs as OKF concept IDs: normalized workspace-relative `.md` paths with the `.md` suffix removed.
- Reject path traversal and paths escaping the workspace root.
- Ignore `.workspace/`, reserved OKF files (`index.md`, `log.md`) as concept documents, temporary editor files, hidden system files where appropriate, and non-`.md` files.
- Implement `NoteRepository` backed by OKF note files, with an `OKFDocument`/`OKFNote` codec at the repository boundary.
- Support list, read, save, delete, and basic rename behavior.
- Preserve filename casing for display.
- Add recent workspace persistence under application state.


Tests:
- Path normalization.
- Workspace-boundary enforcement.
- Metadata directory exclusion.
- Case-collision behavior.
- List/read/save/delete with temporary directories.

Exit criteria:

- The app can open a bundle directory and list non-reserved OKF concept documents by concept ID.
- Notes remain ordinary UTF-8 Markdown `.md` OKF concept documents with YAML frontmatter, readable outside the app by OKF-compatible tools.

## Phase 3: OKF Codec, Parsing, And Link Resolution

Goal: Load, validate, edit, and derive structured facts from OKF notes without making a proprietary content store.

Tasks:

- Parse OKF YAML frontmatter, required `type`, optional standard fields, markdown headings/body, standard Markdown absolute bundle-relative links, relative links, citations, and tags from frontmatter.
- Extract `ParsedOKFNote` with OKF document metadata, title, body/content blocks as applicable, plain text, headings, tags, links, aliases, and modified timestamp.
- Build a dedicated link resolver separate from parser logic.
- Represent unresolved links explicitly.
- Define title fallback order: OKF `title` frontmatter, first body H1/heading, concept ID basename.
- Keep parser tolerant of malformed OKF and metadata/front matter where recovery is possible, while returning structured decode errors when the OKF document cannot be safely interpreted.


Tests:
- Wiki-link parsing.
- Alias parsing.
- Standard Markdown OKF cross-links, including absolute bundle-relative links and relative links.
- Tag extraction.
- Heading extraction.
- Link resolution and unresolved-link handling.

Exit criteria:

- Parsed OKF notes can feed both search documents and graph edges.
- Unresolved links are visible in domain data rather than discarded.

## Phase 4: Bleve Search Adapter

Goal: Provide fast local full-text search as a rebuildable projection.

Tasks:

- Store indexes under `.workspace/search/`.
- Index note ID/path, title, plain-text body, headings, tags, aliases, link targets, and modification timestamp.
- Weight ranking approximately as `title > aliases > headings > tags > body`.
- Support keyword search, multi-word queries, phrase queries, prefix matching, highlighting, tag filters, path filters, limits, and pagination or cursoring.
- Implement incremental index, delete, and complete rebuild.
- Return structured search DTOs for Wails.
- Support context cancellation for stale UI search requests.


Tests:
- Indexing and deletion.
- Title versus body ranking.
- Tag and path filters.
- Highlight extraction.
- Complete rebuild from files.
- Corrupt or missing index recovery path.

Exit criteria:

- Search results open the correct note.
- Rebuilding the search projection never risks note content.

## Phase 5: GraphStore, SQLite Adapter, And Link Inference

Goal: Persist link relationships as a derived graph projection.

Tasks:

- Implement graph domain types for notes, unresolved references, optional tags, and edges.
- Represent `links_to` hard-link edges, optional `inferred_related_to` soft-link edges, and optionally `tagged_with` edges.
- Store graph data in a SQLite database under `.workspace/graph/`, for example `.workspace/graph/graph.sqlite`.
- Implement SQLite adapter behind `GraphStore`.
- Replace a note's outgoing links as atomically as the backend supports.
- Implement outgoing hard links, backlinks, soft-link suggestions, neighborhoods, orphan notes, broken links, full graph filters, tag filters, path-prefix filters, and a hard/soft link visibility filter.
- Keep graph DTOs tailored to frontend rendering and independent from SQLite schema details.

### Link Inference Mechanism

Soft links should be inferred by a local, explainable relatedness pipeline rather than by a hosted AI service. The first implementation should combine deterministic signals into a weighted score:

- Explicit title or alias mentions in another note's body.
- Shared normalized tags, especially rare tags.
- Shared headings, aliases, and extracted keyphrases.
- Bleve/BM25 similarity over title, headings, tags, aliases, and plain text.
- Graph proximity signals such as common neighbors and two-hop paths through hard links.
- Optional future local-only embeddings, kept behind an interface and not required for the first milestone.

Persist only the top-K soft links per note above a configurable threshold. Each soft link must carry a score, evidence list, algorithm version, and last-computed timestamp. Soft links must not be written into authoritative OKF note files unless the user explicitly promotes one into a hard link.


Tests:

- Graph edge replacement.
- Soft-link inference scoring, thresholding, explanation, and rebuild behavior.
- Backlink queries.
- Delete behavior.
- Unresolved-link queries.
- Full graph filtering.
- Persistence after close and reopen.

Exit criteria:

- Graph state can be rebuilt from OKF notes and survives app restart.
- SQLite schema details can evolve without changing application services or frontend DTOs.

## Phase 6: Initial Scan And Rebuild Pipeline

Goal: Build search and graph projections from an existing workspace.

Tasks:

- Implement bounded worker-pool scanning for OKF note files.
- Parse notes and feed search and graph stores.
- Persist enough metadata to detect stale index entries.
- Add a full rebuild command that discards and regenerates derived state.
- Report scan/rebuild progress to the UI.
- Use contexts for cancellation.
- Handle startup after unclean shutdown by validating derived state.


Tests:
- Workspace scan over temporary directories.
- Rebuild after deleted note.
- Stale entry removal.
- Cancellation behavior.
- Partial parse failures do not stop the whole workspace.

Exit criteria:

- Opening a workspace builds usable search and graph projections.
- Derived state can be repaired by rebuild.

## Phase 7: Wails Application APIs

Goal: Expose stable app operations to the frontend.

Tasks:

- Add APIs for opening workspace, listing notes, reading notes, saving notes, deleting notes, search, graph queries, rebuild, recent workspaces, and UI state.
- Return structured `AppError` values with code, message, and optional detail.
- Map domain objects to UI DTOs.
- Add event notifications for workspace loaded, note updated, index progress, graph updated, and external file changes.
- Ensure slow APIs use contexts and do not block indefinitely.

Exit criteria:

- Frontend can perform the vertical slice through Wails-generated bindings.
- Errors are user-actionable and do not expose raw storage-library details.

## Phase 8: React Shell And File Tree

Goal: Provide the first usable workspace UI.

Tasks:

- Build the main layout: toolbar/command search, file tree and search panel, editor pane, backlinks/local graph pane.
- Implement workspace open flow.
- Render file tree grouped by directories.
- Support selecting and opening notes.
- Remember recent workspaces, last opened note, pane sizes, and basic UI state.
- Add keyboard shortcut infrastructure.

Exit criteria:

- User can open a workspace, browse notes, and select a note.
- Restart restores the recent workspace and last note where possible.

## Phase 9: CodeMirror OKF Note Editor

Goal: Edit OKF note files safely and comfortably.

Tasks:

- Integrate CodeMirror 6 with OKF/Markdown-compatible syntax highlighting.
- Add optional line numbers, undo/redo, find/replace, dirty indicator, explicit save shortcut, and debounced autosave.
- Add wiki-link, note-title, and tag autocompletion.
- Add Ctrl/Cmd-click navigation for links.
- Detect external modifications and present a safe resolution flow.
- Start with source-mode OKF editing only.


Tests:
- Save updates disk file.
- Autosave debounces correctly.
- External modification state is detected by backend APIs.

Exit criteria:

- User can edit a note, save it, and see derived search/graph updates.

## Phase 10: Search UI

Goal: Make the Bleve index useful from the desktop app.

Tasks:

- Add search input with cancellation of stale requests.
- Render ranked results with highlighted fragments.
- Add tag and path-prefix filters.
- Support result limits and pagination or incremental loading.
- Open selected result in the editor.
- Keep search responsive during indexing where possible.

Exit criteria:

- User can search across notes, filter results, and open a result.

## Phase 11: Graphology And Sigma Integration

Goal: Render the global and local graph in the frontend.

Tasks:

- Convert backend `GraphDTO` into Graphology data.
- Render with Sigma.js, keeping nodes and labels in WebGL rather than per-node DOM.
- Implement global graph tab/view.
- Implement local graph pane for the selected note.
- Add pan, zoom, reset camera, hover neighborhood emphasis, click to open note, node search, tag/path filters, unresolved/orphan toggles, depth controls, and node-size strategy controls.
- Add semantic zoom label behavior.

Exit criteria:

- User can inspect the global graph and click a node to open its note.
- Local graph updates when the selected note changes.

## Phase 12: ForceAtlas2 Worker And Layout Persistence

Goal: Keep graph layout responsive and durable.

Tasks:

- Run ForceAtlas2 in a Web Worker.
- Load stored coordinates when available.
- Assign positions for new nodes.
- Stop layout after stabilization or a fixed iteration budget.
- Persist coordinates under `.workspace/layout/` behind a layout persistence interface.
- Re-run layout only after material graph changes or explicit user action.


Tests:
- Coordinate persistence and reload.
- New nodes receive positions.
- Layout data remains separate from relationship data.

Exit criteria:

- Graph layout does not block editing or search.
- Coordinates survive app restart.

## Phase 13: Filesystem Watching And Incremental Updates

Goal: Keep derived state synchronized with disk changes from both GoMental and external editors.

Tasks:

- Watch workspace OKF note files, according to the documented OKF file-extension policy, with platform-specific code isolated under `internal/platform`.
- Debounce bursts of file events.
- Handle create, modify, rename, delete, temporary files, partial writes, and stale event ordering.
- Update note repository view, search index, graph projection, and UI notifications.
- Avoid unbounded goroutines by routing events through a bounded indexing pipeline.


Tests:
- Filesystem event coalescing.
- Stale event ordering.
- External modification during editing.
- Rename and delete behavior.
- Incremental index and graph updates.

Exit criteria:

- Changes made outside the app appear without a full restart.
- App-authored saves update projections without duplicate or stale entries.

## Phase 14: Error Recovery And Rebuild Flows

Goal: Make local corruption and edge cases recoverable without endangering notes.

Tasks:

- Define `AppError` codes for inaccessible workspace, decode failure, corrupt search index, corrupt graph store, external conflict, broken rename, missing derived state, and stale derived state.
- Add UI flows to rebuild search and graph projections.
- Keep OKF note files untouched during derived-state repair.
- Add logging suitable for local diagnosis.

Exit criteria:

- Corrupt or missing `.workspace/` projections can be regenerated.
- The app communicates failures clearly enough for a user to act.

## Phase 15: Benchmarks And Performance Tuning

Goal: Measure behavior at realistic scale before optimizing.

Tasks:

- Generate a benchmark workspace with at least 10,000 notes, sparse links, tags, orphans, broken links, and a smaller highly connected cluster.
- Measure initial scan time, index build time, search latency, incremental update latency, graph query latency, graph payload size, and frontend rendering performance.
- Add repeatable benchmark commands and document baseline results.
- Tune worker counts, payload sizes, graph filters, and layout iteration budgets based on measurements.

Exit criteria:

- Baseline performance is documented.
- Major bottlenecks are known before deeper feature work begins.

## First Vertical Slice Acceptance Test

The first working version is complete when this flow succeeds on Windows:

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
13. Restart the application and confirm notes, indexes, graph data, and layout remain available.

## Testing Strategy

Backend integration tests should come before substantial UI work for workspace loading, search indexing, and graph updates.

Required coverage:

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
- Soft-link inference scoring, thresholding, explanation, and rebuild behavior.
- Backlink queries.
- Filesystem event coalescing.
- Stale event ordering.
- Persistence and reload of layout coordinates.

Use temporary directories for integration tests. Do not depend on the developer's real notes workspace.

## First Milestone Non-Goals

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

Keep extension points for local embeddings and vector search. The first vertical slice may implement deterministic soft-link inference, but should not require embeddings or ANN vector search.

## Recommended Immediate Next Steps

1. Run Phase 0 dependency verification for Wails, Bleve, and SQLite graph persistence.
2. Convert the current placeholder app into a Wails app skeleton.
3. Implement the workspace path model and note repository with tests.
4. Establish backend integration tests before building deeper UI surfaces.















