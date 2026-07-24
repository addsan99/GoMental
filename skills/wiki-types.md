---
name: wiki-types
description: The GoMental wiki's type registry — the 11 note types (concept, adr, service, entity, how-to, recipe, meeting, plan, progress, gotcha, convention), each with its audience, required/recommended frontmatter, and body template. The authoritative vocabulary for the `type` frontmatter field.
when_to_use: Read before authoring any note (so you pick the right `type` and fill its expected fields) and when querying by type. type-aware-authoring and wiki-capture both build on this.
---

# Wiki types

The `type` frontmatter field is the wiki's authoritative taxonomy. It is a single lowercase token, it forms a `type:` hub in the graph, and it is **queryable**: `list_notes` accepts a `type` filter (exact, case-insensitive), e.g. list every `adr` or every `gotcha`.

This registry is the vocabulary. Reuse an existing type; introduce a new one only deliberately (adding a type is cheap, but a synonym of an existing type fragments the graph).

## Audience is a property of the type

Each type has an **audience** — who it's primarily written for. This is intrinsic to the type, not stamped per note, so it never drifts. Two orthogonal per-note signals complete the picture:

- **Provenance** — `drafted_by` (who wrote it) and `verified_by` (which human vetted it). This is the real "machine-drafted vs human-confirmed" signal.
- **Escape hatch** — if a *specific* note deviates from its type's default audience, add a reserved tag `audience/human` or `audience/agent`. Reserved namespace, so it stays out of your topical tags.

| audience | types | nature |
|---|---|---|
| **human** | `concept` `adr` `service` `entity` `how-to` `recipe` `meeting` | durable docs written for people (agents read them too) |
| **agent** (durable) | `gotcha` `convention` | agent-first knowledge; also valuable to human devs |
| **agent** (transient) | `plan` `progress` | agent workspace / execution state; retired when the work is done |

To show only human-facing docs, query the human type set (or exclude the `plan/ progress/ gotcha/ convention/` folders). To prime an agent before work in an area, pull that area's `gotcha` and `convention` notes.

## Shared conventions (all types)

- `type` is required. `title` is the only other near-universal field. Everything else is **recommended, not enforced** — keep capture friction low.
- `tags` are **topical only** (`billing`, `retry`, `sqlite`), lowercase kebab-case. Do not encode the type as a tag.
- `repo: <name>` on any note whose content is specific to one code repository (rule 7 in `wiki-conventions.md`) — not just `service` notes. Omit it on conceptual/cross-repo notes.
- `obsolete: true` (+ optional `superseded_by: /replacement`) on any type when a note is stale but kept for its links/history — the UI shows a warning banner (rule 8 in `wiki-conventions.md`). For `plan`/`progress`, retire per their type instead.
- Folder-as-navigation: each type has a home folder (below); the `type` field stays authoritative if they ever disagree.
- Body starts with an H1 matching `title`, then the type's H2 sections. Templates are skeletons, not straitjackets. For diagrams use **mermaid** fenced blocks; for screenshots/exported images use `upload_asset` then insert the returned tag (see `wiki-conventions.md` → "Diagrams and images").

---

## human-facing docs

### `concept` — folder: (root) or `concepts/`
The default. Evergreen explanation of an idea, component, or "how X works."
- Required: `title` · Recommended: `description`, `tags`, `verified_at`
```markdown
---
type: concept
title: <title>
tags: [<topical>]
---
# <title>
> One-line definition.
## Details
## Related
```

### `adr` — folder: `adr/`
Architecture/technical decision record. Immutable in spirit: supersede, don't rewrite.
- Required: `title` · Recommended: `status` {proposed|accepted|superseded|deprecated}, `date`, `supersedes`, `superseded_by`, `deciders`
```markdown
---
type: adr
title: <title>
status: accepted
date: 2026-07-18
superseded_by:
---
# <title>
## Context
## Decision
## Consequences
## Alternatives considered
```

### `service` — folder: `services/`
Profile of a system / microservice.
- Required: `title` · Recommended: `owner`, `repo`, `tier`, `depends_on`, `tags`
```markdown
---
type: service
title: <title>
owner: <team / on-call>
repo: <url or id>
depends_on: [<service ids>]
---
# <title>
> What it does, one line.
## Ownership
## Interfaces        (APIs / events / topics)
## Dependencies
## Related           (entities, dashboards, ADRs)
```

### `entity` — folder: `entities/`
A data entity / domain object: its typed fields and who uses it. Used-by and related links live in the body.
- Required: `title` · Recommended: `description`, `tags`
```markdown
---
type: entity
title: <EntityName>
description: <what it represents>
---
# <EntityName>
> What this entity represents.
## Fields
| field | type | description |
|-------|------|-------------|
| id    | uuid | Primary key |
| …     | …    | …           |
## Used by            (link to service notes)
## Related entities   (link to other entity notes)
```

### `how-to` — folder: `how-to/`
Instructions for a well-defined task, any audience (install X, book a room). Broader than a runbook — no rollback/verification ceremony.
- Required: `title` · Recommended: `audience`, `tags`, `verified_at`
```markdown
---
type: how-to
title: How to <task>
audience: <who this is for>
---
# How to <task>
**Goal:** what you'll achieve.
## Prerequisites      (optional)
## Steps
1. …
## Related
```

### `recipe` — folder: `recipes/`
Cooking recipe, whether hand-authored or imported from schema.org Recipe JSON-LD. Optimized for ingredients, steps, practical timing, source provenance, and later retrieval by cuisine/category/tag.
- Required: `title` · Recommended: `description`, `tags`, `prep_time`, `cook_time`, `total_time`, `servings`, `source_url`
```markdown
---
type: recipe
title: <title>
description: <short description>
tags:
  - recipe
prep_time:
cook_time:
total_time:
servings:
source_url:
---
# <title>

## Summary

Briefly describe the dish, when to make it, and what makes it work.

## Details

- **Prep time:**
- **Cook time:**
- **Total time:**
- **Yield:**
- **Category:**
- **Cuisine:**

## Ingredients

- 

## Equipment

- 

## Instructions

1. 

## Tips

- 

## Variations

- 

## Storage

- 
```

### `meeting` — folder: `meetings/`
Summary of a meeting, optimized for decisions, action items, and reusable context rather than raw transcript storage.
- Required: `title` · Recommended: `date`, `attendees`
```markdown
---
type: meeting
title: <title>
date: <yyyy-mm-dd>
attendees: [<person>]
---
# Meeting Summary: <title>

## Snapshot
- Date:
- Time:
- Attendees:
- Related project:
- Source:

## Summary
Short 3-6 sentence narrative of what happened and why it matters.

## Key Points
- 
- 
- 

## Decisions
- Decision:
  Owner:
  Rationale:
  Impact:

## Action Items
- [ ] Task
  Owner:
  Due:
  Context:

## Open Questions
- 
- 

## Follow-Ups
- Next meeting:
- People to notify:
- Notes to link:

## Context / Background
Useful links, agenda items, prior notes, customer context, project state.

## Raw Import
Optional collapsed/transcript/imported agenda section.
```

---

## agent-first knowledge (durable)

### `gotcha` — folder: `gotcha/`
A focused trap / warning. Agents pull these before touching an area to avoid re-learning a lesson the hard way.
- Required: `title` (state it as the warning) · Recommended: `applies_to` (ids), `tags`
```markdown
---
type: gotcha
title: <the trap, stated as a warning>
applies_to: [<service/entity/area ids>]
---
# <title>
## What goes wrong
## Why
## What to do instead
```

### `convention` — folder: `convention/`
An established way-we-do-it (error handling, feature toggles, messaging). Consult before writing code.
- Required: `title` · Recommended: `applies_to` (ids), `tags`
```markdown
---
type: convention
title: <the convention>
applies_to: [<area/service ids>]
---
# <title>
## The convention
## Rationale
## Example
## Exceptions
```

---

## agent workspace (transient)

### `plan` — folder: `plan/`
The artifact from planning mode: intended approach + steps. Durable enough to resume across sessions; keep it version-stable once approved (let `progress` carry the churn).
- Required: `title` · Recommended: `status` {draft|approved|in-progress|done|abandoned}, `implements` (adr ids)
```markdown
---
type: plan
title: <what we're building>
status: approved
implements: [<adr ids>]
---
# <title>
## Context / Goal
## Approach
## Areas affected      (services, entities, files)
## Risks
## Verification
```

### `progress` — folder: `progress/`
Live task state for one effort, 1:1 with a `plan`. Deliberately transient — this is NOT the in-session task list (agents use their own ephemeral task tool for that); `progress` is for **cross-session / human-visible** state. When the effort completes, distill the outcome into the plan and retire this note.
- Required: `title` · Recommended: `plan` (plan id), `status` {active|complete}, `updated`
```markdown
---
type: progress
title: <effort name> — progress
plan: <plan note id>
status: active
updated: 2026-07-18
---
# <title>
## Done
## In progress
## Pending
## Deferred / Blocked
```
