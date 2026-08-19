# Ingest profiles

An **ingest profile** is an optional per-workspace file at
`<workspace>/.gomental/mapping.yaml`. It describes how a Markdown corpus that
was not authored for GoMental maps onto GoMental's existing concepts, so the
corpus can be read without any corpus-specific logic in the app.

If the file is absent, nothing changes: every accessor on the zero profile is a
no-op and GoMental behaves exactly as it always has.

## What a profile can do

| Capability | Key | Effect |
|---|---|---|
| Frontmatter defaults | `rules[].defaults` | Fill fields the file leaves unset — most importantly the required `type`. A value in the file always wins. When a rule supplies a `type`, a file with no frontmatter at all becomes ingestible and is treated as all body. |
| Alias search fields | `rules[].searchAliases` | Index the values of a frontmatter field into the search index's existing `aliases` field, which is already boosted. |
| Tag fields | `rules[].tagFields` | Fold a frontmatter field's values into the note's tags. They go through the same normalization as authored tags, so the graph's tag hubs, filters and search treat them identically. Only use this for low-cardinality fields — every distinct value becomes a graph hub node. |
| Frontmatter links | `rules[].links` | Treat a frontmatter list field as note-to-note references. They become ordinary links: the resolver, backlinks and graph need no knowledge of where they came from. |
| Exclusions | `exclude` | Drop paths from ingestion entirely. |
| Link prefix stripping | `stripLinkPrefixes` | Remove a leading path prefix from link targets, for corpora that live in a repo subdirectory and write repo-root-absolute links. |

Rules match against the **note id** (the workspace-relative path without the
`.md` extension), not the file path. Globs support `*` (one segment) and `**`
(one or more segments). All matching rules apply, in file order.

## Example

```yaml
version: 1
rules:
  - match: "topics/**"
    defaults:
      type: topic
    searchAliases:
      - keywords
    tagFields:
      - depth
    links:
      - field: depends_on
        strength: hard
        basePath: topics
      - field: relates_to
        strength: soft
        basePath: topics
exclude:
  - "topics/feature-flags/**"
stripLinkPrefixes:
  - "/.github/copilot-instructions"
```

`basePath` is prepended to each reference before resolution, for corpora whose
references are written relative to a subdirectory rather than the workspace
root. `strength` is `hard` (default) or `soft` and maps onto GoMental's existing
link strengths.

## Bundled profile: AI-Overload

`profiles/ai-overload/` contains a ready-made profile for an
[AI-Overload](https://gitlab.com/storm-black/ai-overloads) repo overload, plus a
note type collection for its four document kinds.

Set it up as a **second, read-only workspace** alongside your own notes — the
AI-Overload corpus is treated as frozen and is never written to:

1. Open the corpus as a workspace with root
   `<ai-overloads>/ai-repo-overloads/<repo>/.github/copilot-instructions`,
   using the `readOnlyLocal` access mode.
2. Copy `profiles/ai-overload/mapping.yaml` to
   `<root>/.gomental/mapping.yaml`.
3. Import `profiles/ai-overload/note-types.yaml` as a note type collection so
   `topic`, `expertise`, `procedure` and `tool-overview` get labels and
   templates.
4. Reopen the workspace and rebuild the index.

Measured against the Storm overload (652 files) this yields:

- 196 notes ingested, 0 parse failures (456 auto-generated feature-flag topics
  are excluded).
- 154 topics, 19 tool overviews, 13 procedures, 10 expertise documents.
- 818 frontmatter links (`depends_on`, `extended_by`, `relates_to`), 741
  resolved; the remainder point into the excluded feature-flag topics.
- 1,860 keyword values indexed as searchable aliases.
- 3 tags derived from `depth` (`detail` 94, `deep-dive` 31, `hub` 29), giving
  three graph hub nodes that group topics by how deep they go.

Note what is *not* here: keywords are deliberately indexed as aliases rather
than tags. Across the corpus they yield 3,654 distinct values of which 73% are
used exactly once, so as tags they would bury the graph in single-member hubs.
`depth`, with three values, is the opposite case. The profile lets each field go
where it belongs without either choice being wired into the app.

Without the profile the same corpus ingests **zero** notes: topics fail with
`okf.missing_type` and the other three kinds have no frontmatter at all.
