# GoMental × LLM — "Better Together" Scenarios

Brainstorm of high-value scenarios where an LLM and GoMental combine into a rich **living-wiki platform** used by *both* humans and LLMs: LLMs update notes as they do work, and humans + LLMs read those notes as living documentation.

Status: brainstorm / design notes (no code written). Date: 2026-07-18.

---

## The core insight: more than RAG

GoMental gives an LLM three things it is bad at on its own — and they are exactly what turn a pile of notes into a trustworthy shared brain:

1. **Deterministic, explainable structure.** Hard links, typed OKF notes, tag/type/heading hub edges, backlinks, depth-bounded neighborhoods, orphan/broken-link detection. Where GoMental infers a *soft link* it carries `score + evidence + algo-version + timestamp` (via `Explain()`), so the LLM can reason about *why* two notes relate instead of trusting an opaque similarity score.
2. **Safe multi-writer semantics.** Optimistic concurrency (version token / `If-Match`/412), per-note locks, an append-only audit log, and **API keys that attribute each edit to a specific agent**. This is what makes a wiki that both humans and LLMs write to actually trustworthy.
3. **A live event stream** (SSE `/api/events`) + incremental re-indexing — the wiki is a *reactive* substrate, not a static store.

**Thesis:** GoMental supplies recall, structure, and provenance; the LLM supplies judgment, summarization, and language. The deterministic graph is the LLM's guardrail against hallucinated links; the LLM is the semantic layer the graph can't produce.

**Today's LLM surface** is 7 MCP tools: `search_wiki, read_note, list_notes, create_note, edit_note, backlinks, neighborhood`. Most scenarios below are gated by a small number of **missing MCP tools**, consolidated at the end.

---

## Scenarios

Grouped by which side of the loop they serve.

### WRITE — the flagship loop

#### S1 — Work-journal → living documentation ("capture on completion")
**Value:** Highest-leverage loop. An LLM finishes a task (bug fix, architecture decision, investigation) and distills the *durable* knowledge — decisions, gotchas, "why not X", commands that worked — into a typed OKF note. The next agent or human `search_wiki`s it first instead of re-deriving. This is how the wiki compounds.

- **LLM work — skill `wiki-capture`:** triggered at end of a task. Must (a) distill durable facts from conversational cruft, (b) **search first and decide new-note-vs-update-existing** to avoid duplication, (c) pick the right `type`, (d) link into the graph.
- **GoMental work:** `search_wiki`/`read_note`/`create_note`/`edit_note` ✅; post-save soft-link inference ✅; version token for safe update ✅. **Gap:** per-`type` template/schema registry so captures stay consistent and queryable.

#### S2 — Type-aware structured authoring & querying
**Value:** OKF `type` + frontmatter makes notes semi-structured records (`runbook`, `service`, `adr`, `incident`, `person`). The LLM authors *to a schema* and later answers structural questions ("all services owned by team X", "all ADRs superseded by a newer one"). The `type:`/`tag:` hub nodes make this a graph query, not a grep.

- **LLM work — skill `type-aware-authoring`:** per-type templates; validates required frontmatter fields on write.
- **GoMental work:** metadata (type/tag/heading) hub links ✅; `list_notes` by tag/path ✅. **Gaps:** a **structured query MCP tool** (filter by arbitrary frontmatter field, not just tag); expose the `type:` hub to MCP; a type-schema registry (pairs with S1).

### READ — retrieval that beats flat RAG

#### S3 — Graph-aware answering with citations
**Value:** To answer, the LLM does `search_wiki` → `read_note` the top hits → **follow `backlinks` + `neighborhood`** to pull in the human-curated connected context → answers **citing note IDs**. Exploits the link structure a vector store discards, and can show soft-link *evidence* as part of its reasoning.

- **LLM work — skill `wiki-answer`:** combine full-text + graph traversal; always cite `note_id`; surface soft-link evidence when leaning on an inferred connection.
- **GoMental work:** `search_wiki`/`neighborhood`/`backlinks` ✅. **Gaps:** `expand_context(id, depth)` returning note + neighborhood content in one round-trip (today it's N calls); `explain_link(src, tgt)` to expose `Explain()`.

#### S4 — Onboarding Q&A with gap-capture (closing the loop)
**Value:** A human asks natural questions; the LLM answers from the wiki. On **no answer**, it doesn't just apologize — it (a) asks the expert and captures the answer as a new note, or (b) files a "documentation gap." Usage *reveals* what's missing and the misses *become* content, so the wiki self-heals toward what people actually ask.

- **LLM work — skill `wiki-qa`:** answer-or-capture behavior; writes gap notes.
- **GoMental work:** search + create ✅. **Gap (high value):** log low-confidence / zero-hit queries → a "documentation gaps" backlog. The search index already exists; capturing *failed* searches is a small, powerful addition.

### MAINTAIN — keeping a growing wiki healthy

#### S5 — Knowledge gardener (scheduled hygiene agent)
**Value:** A background agent that keeps the graph clean: finds orphans and broken links, spots stale notes, detects near-duplicates, **promotes high-confidence soft links to hard links** (for human approval), merges dupes, backfills missing tags/types, writes/refreshes MOC ("map of content") index notes. Drudgery no human does — and exactly what stops a wiki from rotting.

- **LLM work — skill `wiki-gardener`:** runs on a schedule; uses graph queries to find problems and propose fixes.
- **GoMental work:** orphan/broken-link/full-graph filters exist in the graph store ✅; soft-link listing + promotion is a designed concept ✅; audit + agent attribution ✅. **Gaps:** MCP tools for `orphans`, `broken_links`, `soft_links(id)`/`link_candidates`, `promote_link`/`add_link`/`remove_link`; a **review-queue / proposed-change** mechanism so edits are human-approvable rather than silently applied.

#### S6 — Contradiction & freshness checking
**Value:** As the wiki grows, notes drift ("we use SQLite" vs a later "migrated to Postgres"). The LLM walks soft-link / neighborhood clusters, cross-checks claims for contradictions and staleness, then flags or reconciles. Uniquely enabled by GoMental clustering *related* notes so the LLM compares a handful, not the whole corpus.

- **LLM work — skill `wiki-consistency`:** traverse clusters, compare claims, flag/reconcile.
- **GoMental work:** soft-link clusters + neighborhood ✅. **Gaps:** freshness/verification metadata convention (`verified_at`, `superseded_by`); a lightweight **annotation/flag** channel so the LLM marks a suspected contradiction without overwriting the note.

### COLLABORATE — human and LLM on the same note, live

#### S7 — Real-time co-editing assistant
**Value:** A human edits a note in the desktop app; the LLM (subscribed to `note-updated` SSE events) reacts in the side pane — suggests links, fixes formatting, flags a contradiction with another note, expands a stub. Optimistic concurrency + agent attribution mean it can't clobber the human and every suggestion is traceable.

- **LLM work — skill `wiki-live-assistant`:** event-driven; offers *suggestions*, never silently rewrites.
- **GoMental work:** SSE events ✅, optimistic concurrency/`If-Match` ✅, agent attribution ✅. **Gaps:** MCP is **stdio-only today** — need an event-subscription transport for agents (HTTP/SSE MCP); a **suggestion/comment channel** + UI side-pane to render agent proposals non-destructively.

#### S8 — Structured ingestion + enrichment
**Value:** Point at an external source (web page, ticket, incident, code, doc); GoMental's importer converts it to OKF; the LLM enriches — summary, `type`/`tags`, links into the existing graph, extracts entities into their own notes. Turns raw material into first-class, linked wiki nodes.

- **LLM work — skill `wiki-ingest`:** consume importer output, structure + link it, extract entities.
- **GoMental work:** importer framework (HTML/JSON-LD/recipe) ✅. **Gaps:** a generic **`import_source(url | raw markdown)` MCP tool** (importers aren't reachable by the agent today); more source recipes.

---

## Cross-cutting: the trust / provenance layer

Underpins every write scenario. Because GoMental has **agent-attributed API keys + append-only audit + optimistic concurrency**, every note can carry honest provenance: *"drafted by agent X on 2026-07-18, verified by human Y."* For a wiki where LLMs write, the "verified-by-human vs machine-drafted" distinction is what makes readers trust it.

- **GoMental work:** expose `history(id)`/audit via MCP; adopt `drafted_by` / `verified_by` frontmatter that the UI surfaces as a badge.

---

## Consolidated work — what to build

### Missing MCP tools (the real bottleneck)

| Tool | Unlocks |
|---|---|
| `explain_link(src, tgt)`, `soft_links(id)` / `link_candidates` | S3, S5, S6 |
| `promote_link` / `add_link` / `remove_link` | S5 |
| `orphans`, `broken_links` | S5 |
| `expand_context(id, depth)` (note + neighborhood text) | S3 |
| structured `graph_query` (filter by frontmatter field) + `type:` hub | S2, S5 |
| `import_source(url \| text)` | S8 |
| `annotate` / `comment(id)` (non-destructive) | S6, S7 |
| `history(id)` / audit | provenance |
| event subscription over MCP (HTTP/SSE transport) | S7 |
| `delete_note` / `rename_note` | S5 |

### Skills to author (LLM side)

`wiki-capture` (S1) · `type-aware-authoring` (S2) · `wiki-answer` (S3) · `wiki-qa` (S4) · `wiki-gardener` (S5) · `wiki-consistency` (S6) · `wiki-live-assistant` (S7) · `wiki-ingest` (S8).

Several share a common core ("search-before-write", "cite note IDs", "distill durable-not-conversational"), so extract a shared **`wiki-conventions`** reference skill first.

### GoMental features beyond MCP

- Per-`type` schema/template registry (S1/S2)
- Failed-query logging → documentation-gaps backlog (S4)
- Proposed-change / review queue (S5)
- Suggestion/comment channel + UI side-pane (S6/S7)
- Freshness/provenance frontmatter conventions + UI badges (cross-cutting)

---

## Recommended first slice

**S1 (`wiki-capture`) + S3 (`wiki-answer`)** — write-then-read is the loop that makes the wiki *grow* and *pay off*. On the GoMental side it needs only `explain_link` and `expand_context`. **S5 (gardener)** is the natural second wave once volume creates mess.
