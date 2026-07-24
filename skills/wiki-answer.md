---
name: wiki-answer
description: Answer a question from the GoMental wiki using graph-aware retrieval — full-text search, then expand into the note's neighborhood, follow backlinks, and answer with cited note ids and evidence-backed connections.
when_to_use: When a question can likely be answered from the wiki — "how does X work", "what did we decide about Y", "where is Z documented", onboarding questions. Also whenever the user asks you to answer "from the wiki" / "from our notes".
---

# wiki-answer (read side of the loop)

Follow `wiki-conventions.md`. Goal: a correct, **cited** answer that exploits the wiki's link structure — not just the top search hit.

## Steps

1. **Search.** `search_wiki` with the user's question (try 1–2 phrasings; use `tags`/`pathPrefix` filters if the area is known). Take the top few hits.

2. **Expand context, don't just read one note.** For the most relevant hit(s), call **`expand_context(id, depth=1)`** — this returns the note's full content *plus* excerpts of its neighborhood in one call. Increase depth (2) only if the answer clearly spans further. Use `backlinks` to see what references the note (often where the "why" lives).

3. **Judge sufficiency.**
   - **Enough info:** answer concisely. **Cite the note id(s)** you used. If you lean on an *inferred* (non-hard) connection, run `explain_link(source, target)` and cite its evidence/summary rather than asserting the link.
   - **Conflicting notes:** surface the conflict and cite both ids; prefer the one with newer/`verified_by` provenance, and flag the drift.
   - **Gap (nothing answers it):** say so plainly. Don't invent. Offer to capture the answer once found (hand off to `wiki-capture`), or point to the closest related notes.

4. **Answer format.** Lead with the direct answer. Follow with supporting detail. End with `Sources:` listing the note ids used (as links/ids the user can open).

## Guardrails
- Never present un-cited wiki claims as fact — if you can't cite a note id for it, it's not "from the wiki".
- Distinguish hard-link facts from inferred/soft relationships (use `explain_link` to be honest about which).
- Read-only: this skill does not modify notes. If the question exposes a documentation gap, note it and defer to `wiki-capture`.
