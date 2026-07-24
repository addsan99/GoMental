---
name: type-aware-authoring
description: Author wiki notes to their type's schema — pick the right `type` from the registry, fill the expected frontmatter and body sections, and validate before saving. Makes notes consistent and queryable by type.
when_to_use: Whenever you create or substantially edit a wiki note. wiki-capture delegates the "how to shape the note" step here. Pair with wiki-types (the registry) and wiki-conventions (the ground rules).
---

# type-aware authoring

Goal: every note is a well-formed instance of a known `type`, so the wiki stays consistent and `list_notes(type=…)` is reliable. The type vocabulary and per-type schemas live in `wiki-types.md` — that is the source of truth; this skill is the procedure.

## Steps

1. **Pick the type.** Choose the best-fit type from `wiki-types.md`. If unsure, confirm the live vocabulary with `list_notes` (each note now carries its `type`) or `search_wiki`. Default to `concept` for evergreen explanation. Reuse an existing type — don't coin a synonym. Only introduce a genuinely new type deliberately (and mention it to the user).

2. **Place it.** Use the type's home folder for the id (`adr/…`, `services/…`, `entities/…`, `how-to/…`, `recipes/…`, `meetings/…`, `plan/…`, `progress/…`, `gotcha/…`, `convention/…`; `concept` at root). The `type` field stays authoritative regardless of folder.

3. **Fill the schema.**
   - Set `type` (required) and `title`.
   - Add the type's **recommended** frontmatter where you know it (e.g. `adr` → `status` + `date`; `service` → `owner`; `plan` → `status`). Recommended ≠ required — don't invent values you don't have; omit rather than guess.
   - Follow the type's **body template** (the H2 sections). Skeletons, not straitjackets: drop a section that genuinely doesn't apply.
   - `tags` are topical only — never encode the type as a tag.
   - If the note is specific to one code repository, set `repo: <name>` (rule 7 in `wiki-conventions.md`). Omit it on cross-repo/conceptual notes.
   - Set `drafted_by` (provenance). Never set `verified_by` on your own writing.
   - For a visual: a **mermaid** fenced block for diagrams, or `upload_asset` → insert the returned `![alt](path)` for a screenshot/exported image (see `wiki-conventions.md` → "Diagrams and images").

4. **Validate before saving.** Check: `type` present and in the registry? `title` set? Any *required* field missing? Body has the type's core sections? Links point at real note ids **as `/`-absolute targets** (so they resolve regardless of the note's folder — see `wiki-conventions.md`)? Fix, then save (`create_note` / `edit_note` with `base_version`). After saving, spot-check a link with `backlinks`.

5. **Audience is automatic.** Don't tag audience — it's a property of the type (see `wiki-types.md`). Only add a reserved `audience/human` or `audience/agent` tag if this specific note deviates from its type's default audience.

## Quick reference (audience by type)
- **human docs:** `concept` `adr` `service` `entity` `how-to` `recipe` `meeting`
- **agent knowledge (durable):** `gotcha` `convention`
- **agent workspace (transient, retire when done):** `plan` `progress`

## Guardrails
- Superseding rather than rewriting? Mark the old note `obsolete: true` with `superseded_by: /new-note` (rule 8) instead of deleting it — the UI banners it and links forward.
- Prefer omitting an unknown recommended field over fabricating it.
- Keep `plan` version-stable once approved; put the churn in its `progress` note.
- One concept per note; link rather than dumping into a catch-all.
- If the content doesn't fit any type well, that's a signal — ask the user whether a new type is warranted rather than forcing a bad fit.
