# GoMental wiki skills (LLM-agnostic)

These are **portable prompt skills** for using a GoMental workspace as a *living wiki* that both humans and LLMs read and write. They are deliberately provider-neutral: plain Markdown with a small YAML frontmatter, referencing only the GoMental MCP tool names. Any agent runtime that can (a) load a system/skill prompt and (b) call MCP tools works — Claude Code, Cursor, Continue, a custom Agent SDK app, etc.

They are **not** under a `.claude/` directory on purpose, so they don't bind to one vendor. (If you *do* use Claude Code and want a skill auto-invocable as `/wiki-capture`, copy that skill into `.claude/skills/<name>/SKILL.md` — the content is identical.)

## Prerequisite: connect to the MCP server

The tools below are the same whether the wiki is a local folder or a shared server
— only *how you connect* differs. Pick the mode that matches your setup:

**A. Local workspace (stdio).** Run GoMental's dependency-free MCP server over a wiki
folder on this machine, and point your agent's MCP client at that process (stdio):

```
gomental mcp --workspace /path/to/wiki
```

**B. Central/remote server (HTTP).** When the wiki is hosted centrally (a team
`gomental serve` instance) and shared with the browser UI, connect your agent's MCP
client to its HTTP endpoint with an API key instead of running a local server:

```
POST https://<host>/mcp        # MCP Streamable-HTTP; Authorization: Bearer <API_KEY>
```

This is the mode to use for a team — browsers and agents all go through the one
server that owns the workspace, so writes stay consistent and conflict-safe. See
**`connect-central-server.md`** for client config (native HTTP and the `mcp-remote`
stdio fallback), roles, and a smoke test.

Either way, the server exposes these tools:

| Tool | Purpose |
|---|---|
| `search_wiki` | Full-text search (ranked, highlighted, tag/path filters). **Start here.** |
| `read_note` | Read a note's full OKF content + a `version` token. |
| `list_notes` | List notes, optionally by path prefix / tag / **type**. Each result carries its `type`. |
| `create_note` | Create a note (`create` / `upsert` / `unique` modes). |
| `edit_note` | Replace content with optimistic concurrency (`base_version`). |
| `upload_asset` | Upload an image (base64) to a note → returns path + `![alt](path)` to insert with `edit_note`. |
| `backlinks` | Notes linking *to* a note. |
| `neighborhood` | Local graph around a note, to a depth. |
| `expand_context` | A note **plus** the content of its neighborhood in one call. |
| `explain_link` | *Why* two notes relate (hard link + evidence + score). |

### Operational notes

- **No delete tool.** The MCP surface can create and edit notes but not delete them. Locally (mode A) you can remove a note by deleting its `.md` file directly (a note's id maps 1:1 to `<workspace>/<id>.md`); the graph/search indexes rebuild from the files on the next server start. Against a central server (mode B) you have no filesystem access — ask a server admin to remove it.
- **Roles (mode B).** Over HTTP, the read tools need a **viewer** API key; the write tools `create_note`/`edit_note`/`upload_asset` need an **editor** key. A write with a viewer key returns a JSON-RPC "insufficient role" error. (Local stdio, mode A, is unauthenticated — it trusts whoever launched the process.)
- **One process per workspace (mode A).** The stdio server takes a lock on the workspace. If another `gomental` process (or a stale one) already holds it, a new server may start but silently produce no output. If tool calls hang or return nothing, ensure no other instance is running against the same workspace. (Mode B has no such issue — the central server *is* the one process, shared by everyone.)
- **Concurrency (mode B).** Others may edit the same notes. Always pass `base_version` from `read_note` to `edit_note`; on a conflict error, re-read and retry instead of force-overwriting.
- **Links must resolve.** Link targets are resolved relative to the linking note's folder, so cross-folder links need a `/`-absolute target — see the "Notes are OKF Markdown" rules in `wiki-conventions.md`. After writing, verify with `backlinks`.
- **Diagrams and images.** Prefer `mermaid` fenced blocks for diagrams (plain text, versionable). For screenshots/exported images, agents call `upload_asset` (base64) then insert the returned `![alt](path)` with `edit_note`; browser users just paste into the editor. See `wiki-conventions.md` → "Diagrams and images".
- **Obsolete notes.** Mark a stale-but-kept note `obsolete: true` (optionally `superseded_by: /replacement`); the reading UI shows a warning banner and agents should not cite it as current. Repo-specific notes carry a `repo:` field. See `wiki-conventions.md` rules 7–8.

## The skills

- **`connect-central-server.md`** — connect an agent to a shared, centrally-hosted wiki over the HTTP `/mcp` endpoint (API keys, roles, `mcp-remote` fallback). Read this first if the wiki is a server URL rather than a local folder.
- **`wiki-conventions.md`** — shared rules every other skill builds on (OKF frontmatter, note ids, search-before-write, cite ids, provenance, audience). Read this first.
- **`wiki-types.md`** — the type registry: the 11 note types (`concept` `adr` `service` `entity` `how-to` `recipe` `meeting` `plan` `progress` `gotcha` `convention`), each with audience + schema. The authoritative `type` vocabulary.
- **`type-aware-authoring.md`** — procedure for authoring a note to its type's schema (right type, fields, body sections, validation).
- **`wiki-capture.md`** (write) — after finishing a task, distill durable knowledge into the wiki.
- **`wiki-answer.md`** (read) — answer a question from the wiki with graph-aware, cited context.

## The loop

`wiki-capture` grows the wiki; `wiki-answer` spends it. Used together, work done once is reusable by the next human or agent instead of re-derived — see `../WIKI_LLM_SCENARIOS.md` for the full scenario map.
