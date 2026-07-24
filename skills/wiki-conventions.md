---
name: wiki-conventions
description: Shared conventions for reading and writing a GoMental wiki via its MCP tools — OKF frontmatter, note ids, search-before-write, citing note ids, and provenance. Other wiki skills build on this.
when_to_use: Load alongside any wiki-* skill. Not usually invoked on its own; it is the shared foundation for wiki-capture and wiki-answer.
---

# Wiki conventions

Ground rules for working with a GoMental wiki (via the `gomental mcp` tools). `wiki-capture` and `wiki-answer` assume these.

## Notes are OKF Markdown

Each note is UTF-8 Markdown with YAML frontmatter. The **only required field is `type`**. Common fields:

```markdown
---
type: <required — one of the registry types; see wiki-types.md>
title: <human title; falls back to first H1, then the note id>
tags: [<lowercase, kebab-case, TOPICAL tags — never the type>]
description: <one-line summary, optional>
repo: <repo name when the note is specific to one code repository, optional — see rule 7>
drafted_by: <agent/human that wrote it, optional — provenance>
verified_by: <human who confirmed it, optional — provenance>
verified_at: <date the content was last confirmed, optional — freshness>
---

# Title

Body in Markdown. Link to other notes with standard Markdown links whose target is the note's id, written as an **absolute path from the workspace root (leading `/`)**:
See [Retry policy](/adr/0007-retry-policy).
```

- **Note id** = workspace-relative path without `.md` (e.g. `services/billing`, `adr/0007-postgres`). Use `/` for folders. The id you pass to `create_note` has **no** leading slash. Keep ids stable — other notes link to them.
- **Links** are standard Markdown links; the target names another note. These become **hard links** (explicit, authoritative) in the graph — *if they resolve to a real id*.
- **Link targets resolve like filesystem paths, relative to the linking note's own folder** — NOT as bare ids from the root. So from `entities/tickets`, a target `(semantic-data-model)` resolves to `entities/semantic-data-model` (dangling), and `(entities/contracts)` resolves to `entities/entities/contracts` (dangling). To link reliably, **prefix the target with `/`** so it is absolute from the workspace root: `(/semantic-data-model)`, `(/entities/contracts)`. (`../` also works, e.g. `(../semantic-data-model)`; a `.md` suffix is optional and ignored.) A bare sibling in the *same* folder resolves fine (`(sib-note)`), but `/`-absolute is the safe default everywhere — always use it for cross-folder links.
- **After writing, confirm links resolved** with `backlinks` on the target (or `explain_link`): a dangling relative target silently produces *no* hard link and will not show up. Do not assume a link worked because the Markdown looks right.
- **`type` is authoritative and queryable.** Pick it from the registry in `wiki-types.md` (the 11 defined types + their schemas); `list_notes` can filter by `type` (exact, case-insensitive), and every listed note carries its type. Reuse an existing type — don't coin a synonym. See `type-aware-authoring.md` for the authoring procedure.
- **Audience is a property of the type, not a per-note tag** (see `wiki-types.md`): `concept`/`adr`/`service`/`entity`/`how-to`/`recipe`/`meeting` are human-facing; `gotcha`/`convention` are agent-first durable knowledge; `plan`/`progress` are transient agent workspace. Only add a reserved `audience/human` or `audience/agent` tag if a specific note deviates from its type's default.

## Hard vs soft vs metadata links

- **Hard link** — you wrote it in the Markdown. Authoritative.
- **Soft link** — GoMental *infers* it (mainly title mentions). A suggestion, never written into a file unless a human/you promote it to a hard link.
- **Metadata link** — shared `tag:`/`type:`/`heading:` hub membership.

`explain_link` tells you which of these connect two notes, with evidence.

## Rules

1. **Search before you write.** Always `search_wiki` (and skim `list_notes`) first. Prefer *updating* an existing note over creating a near-duplicate.
2. **Cite note ids.** When you state something learned from the wiki, cite the id(s) so a human can open the source. When you rely on an *inferred* connection, back it with `explain_link` evidence.
3. **Durable, not conversational.** Capture facts, decisions, and gotchas that will still matter next month — not the play-by-play of one session. If it's only relevant to the current chat, don't write it.
4. **Concurrency-safe edits.** `read_note` returns a `version`; pass it as `base_version` to `edit_note`. On a conflict, re-read and merge — never blindly force-overwrite a note a human may have changed.
5. **Provenance & freshness.** When you author or heavily edit a note, set `drafted_by` in the frontmatter. Leave `verified_by` (and `verified_at`) for a human — don't mark your own work verified. Provenance is the real "machine-drafted vs human-confirmed" signal; audience (above) is a separate, type-level property.
6. **Small, linked notes beat big orphans.** One concept per note; connect it into the graph rather than appending to a catch-all page.
7. **Stamp repo-specific notes with their repo.** When a note's content is tied to one code repository — a service's internals, a repo's build/setup, a `gotcha` or `convention` that only applies inside repo X — set `repo: <name>` in the frontmatter (use the repository's short name, e.g. `repo: aioplatform`, or its URL; be consistent within a wiki). This keeps the source of the knowledge explicit, lets a reader open the right repo, and — in a wiki that spans several repositories — keeps same-named concepts from different repos distinguishable. The `service` type already recommends `repo`; apply the same field to any note that is really about one repo. Omit it for cross-repo/product-level or conceptual notes (a `repo` on those would falsely narrow them).
8. **Mark superseded notes obsolete — don't silently leave stale ones.** When a note's content is no longer current and you're not rewriting it in place, mark it obsolete instead of deleting it (links and history stay intact). Set `obsolete: true` in the frontmatter; add `superseded_by: /path/to/replacement` when a newer note replaces it, and put the reason either in `obsolete_reason:` or as the value itself (`obsolete: "replaced by the v2 billing model"`). The reading UI renders a **warning banner** above the title for any note so marked (a reserved `obsolete` tag is honored too). Rules for using them: don't cite an obsolete note as current fact; when you supersede a note, mark the old one `obsolete` **and** link it forward with `superseded_by`; if you find an obsolete note is actually still correct, clear the flag. `plan`/`progress` notes are the exception — retire those per their type rather than marking obsolete.

## Diagrams and images

Notes render as Markdown in the GoMental UI. You can add visuals two ways — a text **mermaid** diagram or an uploaded **image**. Both are available to agents over MCP as well as to browser users.

### Mermaid diagrams — the default for anything you can draw with boxes and arrows

A fenced code block tagged `mermaid` renders as a diagram (flowchart, sequence, ER, state, …). It is **plain text**, so it lives in the note body, diffs cleanly, versions with the note, and needs no upload — prefer it over an image whenever the visual is a diagram:

    ```mermaid
    flowchart LR
      Ticket -->|assigned to| Agent
      Ticket --> SLA{SLA breached?}
    ```

Prefer mermaid for architecture, flow, sequence, and entity-relationship diagrams. Keep labels short; an `erDiagram` pairs naturally with an `entity` note, a `sequenceDiagram` with a `how-to` or `service`. Because it's text, it also stays searchable and is safe to edit with `edit_note`.

### Images (PNG/JPEG/GIF/WebP/SVG)

Raster/SVG images are stored under `<workspace>/assets/<note-id>/` and referenced with an ordinary Markdown image whose path is **relative to the note's folder**:

    ![Login sequence](../assets/services/billing/login.png)

- **Agents upload with `upload_asset`.** Call `upload_asset(id, file_name, data_base64, mime_type?)` with the image bytes base64-encoded; it stores the file under `assets/<id>/` and returns `{path, markdown}`. Then insert the returned `![alt](path)` tag into the note body with `edit_note` — uploading stores the file but does **not** edit the note for you, so it's a two-step move: `upload_asset` → `edit_note`. Supported types: PNG, JPEG, GIF, WebP, SVG; max 25 MB. (`upload_asset` is a write tool: over the remote `/mcp` endpoint it needs an editor key.)
- **In the browser, just paste.** Paste or drop an image into the GoMental editor and it does the same thing — saves under `assets/<note-id>/` and inserts the `![alt](path)` for you. (The underlying REST call is `POST /api/assets/{id}`.)
- **Prefer mermaid for diagrams; upload for the rest.** Use `upload_asset` for screenshots, photos, or a diagram you exported elsewhere — things that aren't expressible as text. For flowcharts/sequence/ER diagrams, a `mermaid` block is better (editable, searchable, no binary).
- **Always write real alt text.** It's the accessibility label and the only part of an image the search index and graph can see. (`upload_asset` derives a starting alt from the file name — improve it in the inserted tag.)
