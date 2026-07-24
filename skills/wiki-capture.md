---
name: wiki-capture
description: After completing a task, distill the durable knowledge (decisions, gotchas, what worked/why-not) into the GoMental wiki — updating an existing note or creating a well-linked new one — so the next human or agent can reuse it instead of re-deriving it.
when_to_use: At the end of a task that produced knowledge worth keeping — a bug fixed, an architecture decision made, an investigation concluded, a runbook step discovered. Also when the user says "capture this", "write this up", "add to the wiki", or "document this".
---

# wiki-capture (write side of the loop)

Follow `wiki-conventions.md`. Goal: turn what you just learned into durable, linked wiki content — not a transcript.

## Steps

1. **Distill first (in your head).** What are the 1–5 durable facts/decisions worth keeping? Drop anything that only matters to this session. If nothing survives this filter, say so and stop — don't write filler.

2. **Search for a home.** `search_wiki` for the topic (try a couple of phrasings) and skim `list_notes` for the relevant area. Decide:
   - **Update** an existing note if one substantially covers this topic (preferred — avoids duplicates).
   - **Create** a new note only if there's a genuinely new concept.

3. **Choose the `type` and shape the note.** Pick the best-fit type from the registry (`wiki-types.md`) and follow its schema — see `type-aware-authoring.md` for the procedure (right folder, required + recommended fields, body sections). Reuse an existing type; you can confirm the live vocabulary with `list_notes` (it can filter by `type`). Default to `concept` for evergreen explanation.

4. **Write it.**
   - **Update:** `read_note` (keep the `version`) → merge your additions into the content → `edit_note` with `base_version` = that version. On conflict, re-read and re-merge.
   - **Create:** `create_note` with `mode:"create"` (use `"unique"` if the id might collide). Include `type`, a clear `title`, topical `tags`, and `drafted_by`. If the knowledge is specific to one code repository, also set `repo: <name>` (conventions rule 7).
   - Link into the graph: add Markdown links to related note ids you found in step 2, using **`/`-absolute targets** (e.g. `(/entities/contracts)`) so they resolve across folders — see `wiki-conventions.md`.
   - Need a visual? Add a **mermaid** fenced block for a diagram, or `upload_asset` → paste the returned `![alt](path)` tag for a screenshot/exported image (conventions → "Diagrams and images").
   - **Superseding an old note?** If this capture replaces a note rather than updating it, mark the old one `obsolete: true` with `superseded_by:` pointing at the new note (conventions rule 8) — don't leave the stale version un-flagged.

5. **Verify the connections.** After writing, `backlinks` the note and/or `explain_link` between it and a note you linked, to confirm the relationship is real (hard link and/or evidence). Fix dangling links — the usual cause is a non-absolute target that resolved relative to the note's folder and pointed nowhere.

6. **Report.** Tell the user the note id(s) you created/updated and the key facts captured — one or two lines, with the id(s) as citations.

## Guardrails
- Never blindly overwrite: always round-trip `base_version` (conventions rule 4).
- Don't set `verified_by` on your own writing.
- Prefer several small linked notes over one growing dumping-ground note.
- If unsure whether something is durable, ask the user rather than guessing wrong into the wiki.
