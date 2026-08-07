# Suggested Links

## Status

Proposed product and technical design. This feature promotes locally inferred
relationships into explicit, user-authored Markdown links after review or under a
workspace policy.

## Goals

- Find useful related notes from the current, possibly unsaved, note content.
- Explain why every note was suggested.
- Let the user accept, reject, retarget, relabel, or relocate suggestions
  individually or in bulk.
- Persist accepted suggestions as ordinary hard links in the OKF note, so they
  work outside GoMental and participate in backlinks and the graph.
- Support an entirely local, workspace-scoped `off`, `prompt`, or `automatic`
  policy.
- Keep suggestion generation read-only. A note changes only through the normal
  draft and `SaveNote` flow.

## Non-goals for the first release

- Cloud inference or a required embedding service.
- Automatically adding reciprocal links to the suggested target notes.
- Automatically deleting hard links that were accepted previously.
- Rewriting approximate prose matches inline. Inline conversion is limited to
  source spans that a Markdown-aware parser can identify safely.

## Core model

GoMental already distinguishes inferred soft links from explicit hard links.
Suggested links should use that distinction rather than introduce a third stored
relationship type:

1. The suggestion service evaluates the unsaved source note against the current
   workspace corpus.
2. Its results are ephemeral proposals, with score and evidence.
3. Rejected proposals remain derived data plus a suppression decision.
4. Accepted proposals are inserted into the draft as standard Markdown links.
5. The existing save path parses those links and projects them as hard links.

This preserves the note file as the source of truth. It also means accepted links
immediately work with existing backlinks, graph queries, external-change
conflict handling, and rebuilds.

## Finding candidates

### Candidate generation

Generate a bounded candidate set before scoring. Use the union of:

- **Unlinked title mentions:** exact, case-folded mentions of another note's
  title in the draft body. Extend this to aliases if aliases are added to the
  domain model. The existing corpus title matcher can provide this efficiently.
- **Lexical similarity:** query Bleve with the source title, description,
  headings, and a small set of distinctive body terms; retain approximately the
  top 30 results.
- **Shared metadata:** notes sharing tags, type, or uncommon headings. Common
  values must be IDF-weighted so tags such as `general` do not dominate.
- **Graph structure:** notes with shared hard-link neighbors, notes commonly
  co-cited by other notes, and notes that link to the same targets.

Exclude:

- the source note;
- targets already hard-linked anywhere in the source draft;
- unresolved or missing targets;
- reserved notes such as `index` and `log`, unless explicitly mentioned;
- obsolete targets by default;
- a previously rejected source/target pair while its suppression rule applies.

Candidate generation should cap work independently of workspace size. No
request should scan every body in a large workspace.

### Scoring

Use a versioned, deterministic local ranker. Normalize every component to
`0..1` and start with this v2 formula:

```text
score =
    0.45 * lexical_similarity
  + 0.30 * mention_signal
  + 0.15 * metadata_similarity
  + 0.10 * graph_proximity
```

Where:

- `mention_signal` is 1.0 for an exact, unlinked target-title mention and 0
  otherwise. A mention is therefore strong evidence but combines with at least
  some contextual similarity for automatic acceptance.
- `lexical_similarity` is a normalized Bleve score over title, description,
  headings, and body, with title/headings boosted.
- `metadata_similarity` is weighted Jaccard similarity. Tags have the most
  weight, headings less, and type alone contributes no more than 0.05 to the
  final score.
- `graph_proximity` rewards shared hard-link neighbors and co-citation. Existing
  direct links are excluded rather than boosted.

Apply these adjustments after the base score:

- `+0.10` when the source mentions the target title and the target mentions the
  source title;
- `-0.15` for a very short or ambiguous target title;
- `-0.10` when all evidence is from common metadata values;
- clamp the result to `0..1`.

Initial thresholds:

- show in prompt mode at `>= 0.45`;
- label `0.45..0.64` as Possible, `0.65..0.84` as Strong, and `>= 0.85` as High
  confidence;
- automatic mode may accept only `>= 0.85`, up to three links, and only when it
  has an exact title mention or at least two independent evidence families.

These values should be constants in `InferenceConfig`, covered by ranking tests,
and included in an algorithm identifier such as `GoMental-local-v2`. Do not show
raw decimal scores as the primary UX; show confidence and evidence instead.

### Explanation

Each suggestion returns short, user-readable reasons derived from structured
evidence, for example:

- “Mentioned in this note”
- “Similar title and content”
- “Shares tags: go, indexing”
- “Both link to Corpus index”

The explanation must be generated from the same signals used for ranking. The
existing relation explanation logic can supply shared tag/type/heading evidence,
but suggestion ranking should not reuse its additive weights directly.

## Triggering and lifecycle

### While editing

- Debounce for 1,000 ms after the last content change.
- Do not run until the note has at least 80 non-frontmatter characters.
- Recompute only after a meaningful change: at least 40 changed characters, a
  changed title/tag/heading, or an explicit Refresh action.
- Send the current note ID and full unsaved content. Do not require a preliminary
  save.
- Associate results with a `draftHash`; discard a response if the draft changed
  while it was in flight.
- Show proposals in the note's right rail. Computing suggestions never changes
  the draft.

### On save

In prompt mode, Save becomes a two-stage action for a draft hash that has not
already been reviewed:

1. Compute suggestions.
2. If none qualify, save normally.
3. If suggestions qualify, open the review sheet.
4. **Add selected & save** applies the selected transformations to the draft and
   calls the existing `SaveNote` once.
5. **Save without links** records the decision for this draft hash and saves the
   original draft.
6. **Cancel** returns to editing without a write.

Do not save once and then perform a hidden second save. Applying suggestions
before the existing save preserves a single optimistic-concurrency check and a
single user-visible edit.

For a new note, the same flow runs against the proposed ID and content before its
first write. The candidate corpus excludes that ID if it already exists.

### Automatic mode

Automatic mode computes at the configured trigger but only applies links when
the user next saves. It uses the higher threshold and maximum described above.
The Save toast should say, for example, “Saved · added 2 related links” and offer
**Undo**. Undo reapplies the inverse patch and saves only if the note version is
still unchanged.

## Review UX

### Editing panel

Add a **Suggested links** section to the existing right rail, above Backlinks.
Keep it collapsed when there are no results.

```text
Suggested links                                      3

[x] Corpus index                         High confidence
    Mentioned in this note · shares #search
    Add to: Related notes                           Edit

[x] Incremental rebuilds                         Strong
    Similar content · both link to Graph store
    Add to: Related notes                           Edit

[ ] SQLite maintenance                           Possible
    Shares #sqlite
    Add to: Related notes                           Edit

Reject all                    Add selected (2)
```

Selecting a row opens the target note in a preview without discarding the
current draft. Each row supports:

- accept/reject checkbox;
- target preview;
- confidence and explanation chips;
- Edit action for target, label, and placement;
- Reject action with optional “Don't suggest again for this note.”

Bulk actions are **Add selected**, **Add all**, and **Reject all**. “Add all” must
still respect the visible list; it must not accept hidden low-scoring results.

### Save review sheet

Use a modal sheet because Save is awaiting a choice. Reuse the same rows, with
the primary action **Add selected & save**, secondary **Save without links**, and
tertiary **Cancel**. Keyboard behavior:

- Space toggles the focused row.
- Enter previews/edits the focused row.
- Mod+Enter applies selected suggestions and saves.
- Escape cancels the sheet and does not save.

### Editing a proposal

Edit opens the existing link picker, initialized to the proposed target, plus:

- link label (default: target title);
- placement: `Related notes` or a safe inline occurrence when available;
- inline occurrence selector if the exact phrase appears more than once.

Retargeting a proposal keeps it selected but recomputes the duplicate-link check.

## Writing accepted links

### Default: managed Related notes block

V1 should default to a list at the end of the note body:

```markdown
<!-- gomental:related-links -->
## Related notes

- [Corpus index](/architecture/corpus-index.md)
- [Incremental rebuilds](/architecture/incremental-rebuilds.md)
```

Rules:

- Use standard Markdown with a workspace-absolute target and `.md` extension for
  portable OKF output.
- Escape Markdown punctuation in labels and URL-encode unsafe target characters.
- If the marker exists, append missing accepted links to that block.
- If an unmarked `Related notes` heading exists, prompt mode may offer it as the
  destination; automatic mode creates the marked block instead of guessing.
- Preserve user ordering and wording. Never rewrite or remove existing entries.
- Do not include confidence or inference explanations in the saved note.
- Ignore the managed block when computing similarity and title mentions, so
  accepted suggestions do not create a recommendation feedback loop.
- Applying the same accepted proposal twice must be idempotent.

The marker identifies the insertion location, not disposable content. Once a
link is written, it is a normal user-authored hard link.

### Optional: convert an inline mention

Offer inline placement only for an exact target title/alias occurrence that is:

- outside YAML frontmatter, links, images, HTML, code spans, and fenced code;
- represented by a stable source range in the current draft hash; and
- not already part of a hard link.

The transformation preserves the visible phrase:

```markdown
Corpus index
```

becomes:

```markdown
[Corpus index](/architecture/corpus-index.md)
```

The current regex link parser does not retain source spans or exclude all Markdown
literal contexts. Therefore safe inline conversion requires a Markdown-aware AST
or scanner that reports byte ranges. Until that exists, ship Related notes only;
do not perform regex replacement over arbitrary prose.

All accepted transformations should be applied as one descending-range edit set,
then inserted into the editor as one undoable transaction. Rich and source mode
must both receive the resulting full Markdown document.

## Workspace settings

Extend `WorkspaceSettings` with:

```json
{
  "suggestedLinks": {
    "mode": "off | prompt | automatic",
    "trigger": "whileEditing | onSave",
    "placement": "relatedSection | preferInline",
    "minScore": 0.45,
    "maxSuggestions": 5
  }
}
```

Settings UI under **Workspace Settings > Suggested links**:

- **Behavior:** Off / Ask before adding / Add high-confidence links automatically
- **Check for links:** While editing / When saving
- **Preferred placement:** Related notes section / Inline when safe
- Advanced disclosure: minimum confidence and maximum suggestions

Normalization rules:

- invalid values fall back to `off`, `onSave`, `relatedSection`, `0.45`, and `5`;
- clamp `minScore` to `0.30..0.95` and `maxSuggestions` to `1..10`;
- read-only workspace modes force the runtime capability off without erasing the
  saved preference;
- automatic mode always retains the hard-coded `0.85` safety floor regardless of
  a lower display threshold.

For migration, existing workspaces default to `off` to avoid surprising writes.
New workspaces may default to `prompt` + `onSave` after onboarding explains the
feature. There should also be a one-click **Suggest links now** command regardless
of trigger when mode is not off.

## Service and transport design

Suggestion generation is a read-only application service:

```go
type SuggestLinksRequest struct {
    ID      string `json:"id"`
    Content string `json:"content"`
    Limit   int    `json:"limit"`
}

type LinkSuggestionDTO struct {
    TargetID        string            `json:"targetId"`
    TargetTitle     string            `json:"targetTitle"`
    Score           float64           `json:"score"`
    Confidence      string            `json:"confidence"`
    Evidence        []LinkEvidenceDTO `json:"evidence"`
    DefaultPlacement string           `json:"defaultPlacement"`
    InlineRanges    []TextRangeDTO    `json:"inlineRanges,omitempty"`
}

type SuggestLinksResponse struct {
    DraftHash string              `json:"draftHash"`
    Algorithm string              `json:"algorithm"`
    Items     []LinkSuggestionDTO `json:"items"`
}
```

Expose it as:

- Wails: `SuggestLinks(req)`
- HTTP: `POST /api/notes/{id}/link-suggestions`

Add a pure transformation service, shared by rich and source editors:

```go
ApplySuggestedLinks(content, draftHash, accepted[]) -> updatedContent, edits[]
```

It validates the draft hash, rejects stale ranges, deduplicates links, and returns
content without writing it. The frontend then updates its draft and uses the
existing `SaveNote` request with `baseVersion`. This keeps policy and Markdown
rewriting out of the persistence layer.

For on-save prompt mode, do not fold suggestion generation into `SaveNote`; HTTP
clients and agents must retain a predictable explicit-save API.

### Reusing current infrastructure

- Extend `CorpusIndex` candidate generation instead of adding a second corpus
  cache.
- Reuse the title matcher for exact mentions.
- Reuse Bleve for lexical candidates.
- Reuse structured evidence types and `ExplainRelation` metadata intersections.
- Keep the existing background soft-link projection. Its algorithm can be
  upgraded to the same ranker later, but stored soft links and interactive draft
  suggestions have different freshness and latency requirements.

## Rejection memory

Persist suppression decisions in derived workspace state, not note frontmatter.
A SQLite table in the existing graph database is sufficient:

```text
link_suggestion_decisions(
  source_id, target_id, algorithm, decision,
  source_content_hash, decided_at,
  primary key(source_id, target_id, algorithm)
)
```

- A normal rejection suppresses the pair until the source meaningfully changes
  or the algorithm version changes.
- “Don't suggest again for this note” suppresses it until explicitly reset.
- Retargeting and acceptance clear obsolete rejection state.
- Rebuilding derived data may clear ordinary rejections, but the UI must say so;
  durable “don't suggest again” decisions should live in a small workspace
  preference file if rebuilds replace the graph database.

No note content is sent off-device and no suggestion text is logged.

## Error, stale-result, and conflict behavior

- A suggestion failure never blocks saving; show “Suggestions unavailable” and
  retain **Save without links**.
- Results whose `draftHash` differs from the current draft are never applicable.
- A missing/renamed target is disabled and explained in the review UI.
- If `SaveNote` reports an external conflict, keep the review decisions but
  recompute transformations after the user resolves the content conflict.
- If the projection/corpus is still rebuilding, show a nonblocking loading state
  and let Save proceed.
- Cancel outstanding edit-time requests when the selected note changes.

## Performance targets

- Edit-time debounce work should start within 1.2 seconds of idle.
- P95 suggestion response under 250 ms at 10,000 notes and under 750 ms at
  100,000 notes on the reference Windows machine.
- Candidate set at most 100 before scoring; return at most 10.
- Requests must be cancellable and must not block `SaveNote` or the inference
  worker.

## Testing

### Ranker

- exact unlinked title mention ranks above metadata-only similarity;
- rare shared tags outrank common tags;
- type alone never reaches the prompt threshold;
- already-linked, self, obsolete, and reserved candidates are excluded;
- deterministic tie-breaking by target ID;
- incremental/indexed candidates match a brute-force reference corpus.

### Markdown transformation

- creates and appends to the managed section;
- remains idempotent;
- preserves frontmatter, CRLF normalization policy, unknown metadata, and user
  content;
- escapes labels and targets;
- does not modify code, existing links, images, HTML, or frontmatter;
- rejects stale hashes and overlapping edit ranges;
- produces links that the existing OKF parser resolves to the intended IDs.

### Application and transport

- unsaved drafts receive suggestions;
- new-note IDs are handled;
- HTTP and Wails responses are equivalent;
- read-only mode does not apply changes;
- external save conflicts remain detectable;
- suggestion errors do not prevent a normal save.

### UI

- accept/reject/edit one and all;
- Save without links and Cancel have distinct behavior;
- stale results disappear after edits;
- keyboard and screen-reader behavior;
- source and rich editors apply the same full-document result;
- automatic additions are disclosed and undoable.

## Rollout

1. **Foundation:** read-only `SuggestLinks`, v2 ranker, explanations, tests, and a
   manual Suggested links panel. No automatic changes.
2. **Prompt on save:** managed Related notes insertion, bulk review, rejection
   memory, and optimistic-concurrency integration.
3. **While editing and automatic mode:** debounce/cancellation, high-confidence
   gate, save disclosure, and Undo.
4. **Safe inline conversion:** introduce Markdown source ranges, previews, and
   placement editing.
5. **Optional semantic enhancement:** evaluate a local embedding index only if
   lexical quality is insufficient; keep the feature fully offline.

## Product acceptance criteria

- A user can see why each note was suggested before accepting it.
- Accepting one or many suggestions produces resolvable standard Markdown hard
  links in exactly one save.
- Rejecting or cancelling does not alter the note.
- Automatic mode never accepts metadata-only suggestions, never adds more than
  three links per save, and always discloses what changed.
- The capability is independently configurable for every workspace and is
  effectively disabled in read-only workspaces.
- Existing save conflicts, backlinks, graph projection, and rebuild behavior
  continue to work without a special suggested-link persistence path.
