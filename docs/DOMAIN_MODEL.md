# GoMental Domain Model

This document describes the DDD-style domain model for GoMental, a local-first desktop knowledge-management application. It intentionally separates domain concepts from Wails, React, Bleve, SQLite, Graphology, Sigma, filesystem implementation details, and concrete OKF codec/parser libraries.

## Domain Vision

GoMental helps a user maintain a local knowledge workspace made of ordinary local OKF note files. Google's **Open Knowledge Format (OKF) v0.1 draft** concept documents are the source of truth. Search indexes, graph stores, layout coordinates, and UI state are derived projections that can be rebuilt without risking note content.

## Bounded Contexts

### Workspace Context

Owns workspace selection, workspace-relative note identity, OKF note file discovery, note persistence, metadata-directory exclusion, and recent-workspace state.

Primary concepts:

- `Workspace`
- `WorkspaceRoot`
- `WorkspaceMetadataDirectory`
- `NoteID`
- `NotePath`
- `NoteRepository`
- `WorkspaceScanner`

### Note Parsing Context

Owns conversion from OKF note text into structured note facts used by search and graph projections.

Primary concepts:

- `Note`
- `ParsedOKFNote`
- `OKFMetadata`
- `Heading`
- `Tag`
- `ParsedLink`
- `LinkResolver`

### Search Context

Owns the local full-text search projection. Search is rebuildable from parsed OKF notes and must not become the source of note content.

Primary concepts:

- `SearchDocument`
- `SearchQuery`
- `SearchResult`
- `SearchIndex`
- `SearchProjectionRebuilder`

### Graph Context

Owns the persistent relationship projection derived from hard links, inferred soft links, and tags. Graph data is rebuildable from OKF notes.

Primary concepts:

- `Graph`
- `GraphNode`
- `GraphEdge`
- `NoteLink`
- `InferredNoteLink`
- `LinkEvidence`
- `LinkInferenceService`
- `GraphStore`
- `GraphFilter`
- `GraphProjectionRebuilder`

### Indexing Context

Owns the pipeline that turns workspace events into updated search and graph projections.

Primary concepts:

- `WorkspaceEvent`
- `IndexingJob`
- `IndexingPipeline`
- `ProjectionState`
- `RebuildOperation`

### Layout Context

Owns persisted graph coordinates and other derived graph-view state. Layout is UI state, not graph truth.

Primary concepts:

- `LayoutCoordinates`
- `LayoutStore`
- `LayoutSnapshot`

### Application Context

Coordinates use cases across bounded contexts and maps domain objects to Wails-facing DTOs.

Primary concepts:

- `ApplicationService`
- `WorkspaceService`
- `NoteService`
- `SearchService`
- `GraphService`
- `AppError`
- `ApplicationEvent`

## Ubiquitous Language

| Term | Meaning |
| --- | --- |
| Workspace | A user-selected local directory containing OKF notes. |
| Note | An OKF concept document: a UTF-8 Markdown `.md` file with YAML frontmatter inside the workspace/bundle. |
| Note ID | The OKF concept ID: normalized bundle-relative path with `/` separators and the `.md` suffix removed. |
| Source of truth | The OKF `.md` concept document on disk. |
| Derived projection | Rebuildable data produced from notes, such as search indexes, graph edges, and layout coordinates. |
| Parsed OKF note | Structured representation extracted from an OKF document. |
| Link | A relationship between notes. Hard links are explicit standard Markdown links in OKF bodies; soft links are inferred local relationships. |
| Hard link | An explicit user-authored standard Markdown OKF cross-link, either absolute bundle-relative or relative. |
| Soft link | A locally inferred relationship between notes, stored as rebuildable derived data with score and evidence. |
| Resolved link | A link whose target maps to an existing note. |
| Unresolved link | A link target that does not currently map to an existing note. |
| Backlink | A hard note-to-note relationship where another note explicitly links to the current note. Soft-link backlinks may be shown separately as suggestions. |
| Orphan note | A note with no incoming or outgoing note links. |
| Local graph | A graph centered on the selected note and its neighborhood. |
| Global graph | A filtered graph view for the workspace. |
| Rebuild | Discarding and regenerating derived projections from OKF note files. |

## Aggregates

### Workspace Aggregate

Represents an opened local directory and its application-owned derived state.

Suggested fields:

```go
type Workspace struct {
    Root        WorkspaceRoot
    MetadataDir WorkspaceMetadataDirectory
    OpenedAt    time.Time
}
```

Responsibilities:

- Validate that the root exists and is accessible.
- Provide safe resolution from `NoteID` to absolute paths.
- Prevent path traversal outside the root.
- Identify application-owned metadata paths.
- Provide workspace-level policies such as case handling.

Invariants:

- A `NoteID` must never resolve outside the workspace root.
- Application metadata must be excluded from note scanning.
- Derived state must live under the configured metadata directory.
- Workspace policies must be consistent for the lifetime of the opened workspace.

### Note Aggregate

Represents the editable OKF note document as source content plus identity metadata.

Suggested fields:

```go
type Note struct {
    ID         NoteID
    Path       NotePath
    Document   OKFDocument
    Body       string
    ModifiedAt time.Time
    Version    FileVersion
}
```

Responsibilities:

- Preserve the OKF document and its editable body/content as authored by the user.
- Track file version or modification state for conflict detection.
- Provide data needed to save safely.

Invariants:

- A note is stored as OKF, not as proprietary database content.
- A note belongs to exactly one workspace.
- Saving a note must not write outside the workspace root.
- External modifications must be detected before overwriting when possible.

### ParsedOKFNote Aggregate

Represents facts extracted from a note for projections.

Suggested fields:

```go
type ParsedOKFNote struct {
    ID         NoteID
    Document   OKFDocument
    Title      string
    Body       string
    PlainText  string
    Headings   []Heading
    Tags       []Tag
    Links      []ParsedLink
    ModifiedAt time.Time
}
```

Responsibilities:

- Capture OKF metadata, headings, tags, links, aliases, body/content blocks as applicable, and plain text.
- Feed search and graph projections.
- Preserve unresolved links as explicit facts.

Invariants:

- Parsing must be tolerant of malformed OKF.
- Link resolution rules do not belong inside OKF parsing.
- Parsed data is rebuildable and must not be the sole copy of user content.

### Graph Aggregate

Represents a graph projection returned by graph queries.

Suggested fields:

```go
type Graph struct {
    Nodes []GraphNode
    Edges []GraphEdge
}
```

Responsibilities:

- Represent existing notes, unresolved references, optional tag nodes, and optional inferred soft-link relationships.
- Represent `links_to` hard-link edges, optional `inferred_related_to` soft-link edges, optional `tagged_with` edges, and future local semantic-similarity edges.
- Support graph-specific queries without exposing storage backend details.

Invariants:

- Hard note-to-note edges are derived from parsed OKF links. Soft note-to-note edges are inferred from local note signals and remain derived projections.
- Unresolved references remain visible as unresolved nodes or explicit broken-link records.
- Replacing a note's outgoing hard-link edge set should be atomic or as close as the storage backend permits.
- Replacing a note's inferred soft-link set should be versioned by inference algorithm and threshold so stale suggestions can be discarded.

### LayoutSnapshot Aggregate

Represents persisted graph coordinates for UI rendering.

Suggested fields:

```go
type LayoutSnapshot struct {
    WorkspaceID string
    Coordinates map[string]LayoutCoordinates
    UpdatedAt   time.Time
}
```

Responsibilities:

- Persist graph coordinates separately from graph relationships.
- Allow new nodes to receive initial positions.
- Survive app restarts.

Invariants:

- Layout state must not be required to reconstruct graph relationships.
- Missing layout data should degrade to generated initial positions.

## Entities

### NoteSummary

A lightweight listing record for file trees and search results.

```go
type NoteSummary struct {
    ID         NoteID
    Title      string
    Path       NotePath
    Tags       []Tag
    ModifiedAt time.Time
}
```

### Heading

```go
type Heading struct {
    Level int
    Text  string
    Slug  string
}
```

Rules:

- `Level` is normally 1 through 6.
- `Text` is the rendered heading text.
- `Slug` is useful for same-note or heading-specific links.

### ParsedLink

```go
type ParsedLink struct {
    Source      NoteID
    RawTarget   string
    ResolvedID  *NoteID
    DisplayText string
    Heading     string
    Kind        LinkKind
    Strength    LinkStrength
}
```

Rules:

- `ResolvedID` is nil for unresolved links.
- Alias display text is captured separately from the raw target.
- Standard OKF links and wiki links may use different `Kind` values.
- Parsed OKF-authored links are hard links.

### NoteLink

Graph-ready representation of a note relationship.

```go
type NoteLink struct {
    Source      NoteID
    Target      string
    ResolvedID  *NoteID
    DisplayText string
    Heading     string
    Strength    LinkStrength
}
```


### InferredNoteLink

Graph-ready representation of a soft link inferred from local note signals.

```go
type InferredNoteLink struct {
    Source       NoteID
    Target       NoteID
    Score        float64
    Evidence     []LinkEvidence
    Algorithm    string
    ComputedAt   time.Time
}

type LinkEvidence struct {
    Kind   EvidenceKind
    Detail string
    Weight float64
}
```

Rules:

- Soft links are derived data and must be rebuildable.
- Soft links must not be written into OKF notes unless the user explicitly promotes them to hard links.
- Scores should be bounded and comparable within an algorithm version.
- Evidence should be human-readable enough to explain why the link was inferred.

### LinkStrength

```go
type LinkStrength string

const (
    LinkStrengthHard LinkStrength = "hard"
    LinkStrengthSoft LinkStrength = "soft"
)
```

### GraphNode

```go
type GraphNode struct {
    ID         string
    Label      string
    Kind       GraphNodeKind
    NoteID     *NoteID
    Tags       []Tag
    ModifiedAt *time.Time
}
```

Kinds:

- `note`
- `unresolved`
- `tag`

### GraphEdge

```go
type GraphEdge struct {
    ID     string
    Source string
    Target string
    Kind   GraphEdgeKind
}
```

Kinds:

- `links_to` hard link
- `inferred_related_to` soft link
- `tagged_with`
- `semantic_similarity` reserved for later local-only semantic inference.

## Value Objects

### WorkspaceRoot

Validated absolute filesystem path for the opened workspace.

Rules:

- Must exist and be a directory.
- Must be accessible for reading.
- Must be writable for note editing in normal operation.

### WorkspaceMetadataDirectory

Configurable application-owned directory under the workspace, initially `.workspace/`.

Rules:

- Must be excluded from note scanning.
- Contains derived data only: search, graph, layout, and state.

### NoteID

Workspace-relative, normalized path used as note identity in the first version.

Rules:

- Uses `/` separators.
- Must not be absolute in stored domain form; absolute bundle-relative OKF links beginning with `/` are resolved into concept IDs.
- Must not contain traversal that escapes the workspace/bundle.
- Preserves actual filename casing for display.
- On Windows, collision detection should treat IDs case-insensitively unless a different policy is documented.

### OKFDocument

The decoded representation of an OKF v0.1 concept document.

Rules:

- Must preserve user-authored YAML frontmatter, including unknown producer-defined keys, and the Markdown body for round-tripping.
- Must expose normalized `type`, `title`, `description`, `resource`, `tags`, `timestamp`, unknown metadata, body/content, citations, and links to domain services.
- Must not force derived search or graph data back into the authoritative OKF note unless OKF explicitly defines that field as user-authored note content.

### OKFMetadata`r`n`r`nFrontmatter metadata for an OKF v0.1 concept document.`r`n`r`nRules:`r`n`r`n- `type` is required and must be non-empty for conformant concept documents.`r`n- `title`, `description`, `resource`, `tags`, and `timestamp` are optional standard fields.`r`n- Unknown producer-defined keys must be tolerated and preserved when possible.`r`n- Reserved files `index.md` and `log.md` have special meaning and are not ordinary concept documents.`r`n`r`n### NotePath

Presentation-friendly path data derived from `NoteID` and filesystem casing.

### Tag

Normalized tag value extracted from Markdown, without the leading `#` in domain code.

### FileVersion

A value object used to detect external modifications. It may include modified time, size, content hash, or platform-specific identity details.

### SearchQuery

```go
type SearchQuery struct {
    Text       string
    Tags       []Tag
    PathPrefix string
    Limit      int
    Cursor     string
}
```

Rules:

- Empty or whitespace-only queries should either return recent notes or no results, based on UI decision.
- Limit must be bounded by an application maximum.
- Query execution must be cancellable.

### GraphFilter

```go
type GraphFilter struct {
    Tags             []Tag
    PathPrefix       string
    IncludeUnresolved bool
    IncludeOrphans    bool
    Depth            int
}
```

Rules:

- Depth must be bounded.
- Full graph payloads may still be filtered to protect UI responsiveness.

### LayoutCoordinates

```go
type LayoutCoordinates struct {
    X float64
    Y float64
}
```

## Domain Services

### LinkResolver

Resolves parsed link targets to notes.

Responsibilities:

- Resolve wiki-link targets.
- Resolve aliases while preserving display text.
- Resolve relative and absolute workspace-style paths.
- Apply documented case policy.
- Return explicit unresolved results.

### OKFParser

Extracts structured facts from OKF note text.

Responsibilities:

- Parse OKF YAML frontmatter, Markdown body, headings, frontmatter tags, standard Markdown links, citations, and plain text.
- Avoid filesystem lookups and target-resolution policy.

### WorkspaceScanner

Discovers OKF notes in a workspace.

Responsibilities:

- Exclude metadata and ignored files.
- Emit candidate notes for parsing and indexing.
- Support cancellation and bounded concurrency.

### IndexingPipeline

Coordinates updates from filesystem events or app-authored saves.

Responsibilities:

- Debounce bursts.
- Read files after writes settle.
- Parse OKF.
- Update search projection.
- Update graph projection.
- Notify application/UI subscribers.

### ProjectionRebuilder

Rebuilds search and graph projections from OKF note files.

Responsibilities:

- Discard corrupt or stale derived state.
- Recreate projections from the authoritative workspace.
- Report progress and support cancellation.

## Repositories And Ports

### NoteRepository

Reads and writes OKF notes from the workspace filesystem.

### SearchIndex

Indexes and queries `SearchDocument` values. Bleve is the first adapter.

### GraphStore

Stores and queries graph relationships. SQLite is the first adapter, using `modernc.org/sqlite` as the Phase 0-selected Go driver.

Required operations:

```go
type GraphStore interface {
    ReplaceOutgoingLinks(ctx context.Context, source NoteID, links []NoteLink) error
    DeleteNote(ctx context.Context, id NoteID) error
    OutgoingLinks(ctx context.Context, id NoteID) ([]NoteLink, error)
    Backlinks(ctx context.Context, id NoteID) ([]NoteLink, error)
    Neighborhood(ctx context.Context, id NoteID, depth int) (Graph, error)
    FullGraph(ctx context.Context, filter GraphFilter) (Graph, error)
}
```

The domain depends on `GraphStore`, not on SQL tables, SQL queries, or a concrete SQLite driver. Soft links remain derived graph rows owned by the SQLite adapter and link inference pipeline, while hard links are explicit OKF-authored relationships.

### LayoutStore

Persists graph coordinates. Compact JSON is acceptable for the first version.

### AppStateStore

Persists recent workspaces, last opened note, pane sizes, and basic UI state.

### FileWatcher

Emits workspace filesystem events. Windows implementation should be isolated behind this port.

## Application Services

### WorkspaceService

Use cases:

- Open workspace.
- Close workspace.
- List recent workspaces.
- Rebuild derived state.
- Report workspace status.

### NoteService

Use cases:

- List notes.
- Read note.
- Save note.
- Delete note.
- Rename note later if not included in the first vertical slice.

### SearchService

Use cases:

- Search notes.
- Cancel stale search requests.
- Rebuild search index.

### GraphService

Use cases:

- Get global graph.
- Get local graph.
- Get backlinks.
- Get soft-link suggestions and explanations.
- Promote a soft link into an explicit hard link by editing the OKF note through note services.
- Get broken links.
- Rebuild graph projection.

### LayoutService

Use cases:

- Load coordinates.
- Save coordinates.
- Clear layout for explicit re-layout.

## Domain Events

Suggested events:

```go
type WorkspaceOpened struct { Root WorkspaceRoot }
type NoteCreated struct { ID NoteID }
type NoteModified struct { ID NoteID }
type NoteDeleted struct { ID NoteID }
type NoteRenamed struct { OldID, NewID NoteID }
type NoteParsed struct { ID NoteID }
type ProjectionUpdated struct { ID NoteID }
type RebuildStarted struct { Kind ProjectionKind }
type RebuildProgressed struct { Kind ProjectionKind; Completed, Total int }
type RebuildCompleted struct { Kind ProjectionKind }
type RebuildFailed struct { Kind ProjectionKind; Err AppError }
type ExternalModificationDetected struct { ID NoteID }
```

Events should be internal application facts first, then mapped to Wails events for the frontend.

## Invariants And Policies

- OKF note files are authoritative.
- Bleve, SQLite graph data, layout data, and app state are derived or auxiliary stores.
- Derived state can be deleted and rebuilt.
- No domain service should depend on Wails or React types.
- No frontend DTO should become a domain object.
- File paths must be normalized before use as note IDs.
- Paths escaping the workspace root must be rejected.
- Link resolution must preserve unresolved references.
- Hard links and soft links must be distinguishable in domain data, graph storage, DTOs, and UI filters.
- Soft links must include score, evidence, and algorithm version.
- Soft links must not affect authoritative backlinks unless promoted to hard links by the user.
- Slow operations must accept `context.Context`.
- Worker pools must be bounded.
- Application errors should be structured and user-actionable.

## Adapter Mapping

| Domain Port | First Adapter | Storage Location |
| --- | --- | --- |
| `NoteRepository` | Local filesystem OKF note files through `OKFCodec` | Workspace root |
| `SearchIndex` | Bleve | `.workspace/search/` |
| `GraphStore` | SQLite database for hard and soft graph edges via `modernc.org/sqlite` | `.workspace/graph/graph.sqlite` |
| `LinkInferenceService` | Local weighted relatedness engine using search, tags, aliases, keyphrases, and hard-link topology | `.workspace/graph/` or `.workspace/state/` |
| `LayoutStore` | JSON or compact local format | `.workspace/layout/` |
| `AppStateStore` | JSON or platform app state | `.workspace/state/` and/or user app config |
| `FileWatcher` | Windows filesystem watcher | N/A |

## Server & Agent Concepts (Phases 16–24)

These are adapter/operational concepts layered over the transport-agnostic core; they do not change note content and stay out of OKF (Guardrail G4).

- **Host** (`internal/apphost`): owns the single `Service` per process plus an event **Hub** (fan-out to N subscribers with slow-consumer drop) and an **Environment** capability set (native dialogs on desktop, off in server/headless).
- **Actor / Role**: an identity (`viewer` < `editor` < `admin`) resolved per request by a pluggable **Authenticator**. Default is **trust-all** (single local admin, not enforced). Authorization is an adapter concern; the core Service is auth-unaware.
- **API Key**: a revocable credential (SHA-256-hashed at rest under `.workspace/server/`) tied to a role, presented as a Bearer token; attributes agent requests.
- **AuditEntry / AuditLog**: append-only JSONL record (who/when/action/note id/version/result) under `.workspace/audit/`, rotated by size.
- **Version token** (`FileVersion` → `<modUnixNano>-<size>`): opaque optimistic-concurrency token surfaced on `NoteDTO.Version`; the conflict path uses `ErrExternalConflict`.
- **New domain events** already emitted by the core and fanned out to SSE clients: `note:updated`, `note:deleted`, `graph:updated`, plus lifecycle/index events.

## Open Design Decisions

- OKF specification source/version/profile and file extension policy.
- Final Go module path.
- Final Wails command layout: keep `cmd/GoMental` or move to `cmd/app`.
- Migration tooling/schema versioning approach for SQLite graph storage.
- Exact case-sensitivity behavior for note identity on Windows.
- Initial soft-link scoring weights, threshold, and top-K limits.
- Whether tags are graph nodes in the first vertical slice or enabled after note-link graph stability.
- Whether empty search returns recent notes or no results.
- How aggressive full-graph payload limiting should be for very large workspaces.














