# Graph View Rework — Plan

Status: **approved — in execution**. Captures the agreed direction and a phased
implementation plan for reworking the graph view. Decisions here are starting
points; we expect to revise as we see results.

## Goals (from the user)

1. **More visualization types** — radial, drawn zone/region boundaries (hulls),
   explicit grouping.
2. **Scalable, graceful degradation** on large graphs.
3. **Flexible subgraph selection** — not only distance-from-current-note, but
   metadata (type / folder / tags) so users can view the most relevant subset.
4. **More intuitive controls** — kill the "wait-then-center" jump; unify
   zoom/pan/rotate across 2D and 3D.

## Locked decisions

- **Scale target:** optimise for ≤ ~2K nodes (the common case); degrade
  gracefully to 10K+ (rare). We may **auto-disable 3D** and **auto-aggregate**
  above a node-count threshold.
- **"Boundaries" = drawn hull/zone outlines** around metadata groups. No vector
  embedding pipeline.
- **First-class facets:** `type`, `tags`, folder/`path`. All already projected
  in SQLite (`notes` / `note_tags`) — **no new backend schema** needed.
  Arbitrary frontmatter (e.g. `repo`) is out of scope (lives in
  `OKFMetadata.Unknown`, unprojected).
- **3D stays**, invested in equally with 2D, but auto-downgraded past a size
  threshold.
- **Selection camera:** gentle auto-center on select (smooth ease, no
  settle-wait, no end-of-sim jump).
- **Aggregation:** automatic past a threshold (no manual opt-in), threshold
  tunable (~1.5–2K default).
- **Filter semantics (Q1):** focus + context — matches are emphasised, the rest
  **dim + shrink** (not deleted); a "hide non-matches" toggle is available.
- **Filter composition (Q2):** filters compose; a focus note is just another
  predicate. "Playbooks within 2 hops of this note" and "all playbooks" use one
  mechanism, not two modes.

## Relevance model (drives filtering)

The workspace mixes intents; `type` already encodes them almost 1:1:

| Intent                     | Signal                          |
|----------------------------|---------------------------------|
| introductory / newcomers   | `type: intro`                   |
| task-oriented              | `type: playbook`                |
| general information        | `type: concept`                 |
| detailed drilldowns / record | `type: decision`, `type: policy` |

Three complementary axes, all already queryable:
- **Intent** = `type` · **Area** = folder · **Topic** = `tags`
- plus **structure** = hops-from-focus, degree/centrality, `modified` recency.

Filtering narrows along one or more axes; relevance is expressed as **emphasis**,
not just presence.

---

## Current architecture (baseline)

- **Backend** (`internal/graph/sqlite.go`, `internal/application/service.go`):
  `Neighborhood(id, depth)` (recursive CTE, hard/soft only — metadata hubs
  excluded) and `FullGraph(filter)` (all notes + edges, metadata gated by
  filter). Flattened to `GraphDTO {nodes, edges}` by `graphDTO()`.
- **Frontend:** `frontend/src/ui/GraphView3D.tsx` — a single
  `react-force-graph-3d` (Three.js) component used **twice**: `flat` (top-down
  2D, persists `{x,y}` layout) and orbit (3D, transient). d3-force is the only
  layout. `assignHops()` already BFS-labels hop distance.
- **Dead code:** `frontend/src/ui/GraphView.tsx` (retired Sigma.js 2D view) is
  **not imported anywhere** — safe to delete.
- **Known gap:** the "Metadata" link toggle is a no-op in Neighborhood (the CTE
  never returns metadata edges).

---

## Target architecture

Two seams introduced so layouts, renderers, and queries evolve independently:

1. **`LayoutEngine`** — `(graph, ctx) → positions`. Implementations: `force`
   (wrap current d3), `radial` (deterministic from `hops`), `zoned`
   (group-by-facet attraction + hull polygons). Deterministic layouts pin
   `fx/fy`; force runs live.
2. **Renderer adapter** — `react-force-graph-2d` (canvas) for the flat/2D view
   (lighter, simpler controls, easy canvas hull overlay); `react-force-graph-3d`
   for orbit. Shared graph-model builder, palette, and hops/degree logic
   extracted into a common module both consume.
3. **`GraphQuery`** (backend) — `{seed?, depth?, types[], tags[], pathPrefix?,
   linkTypes, includeUnresolved}`. `Neighborhood` and `FullGraph` become presets
   over it.

---

## Phases

### Phase 0 — Foundation & cleanup
- Delete dead `GraphView.tsx` (+ any now-unused Sigma deps).
- Extract shared graph core (DTO→model, hops, degree, palettes, stats) out of
  `GraphView3D.tsx`.
- Introduce the `LayoutEngine` and renderer-adapter seams (no behaviour change
  yet — force layout wrapped as the first engine).

### Phase 1 — Layout strategies (Goal 1)
- Implement `radial` (rings by hop distance; angular slots grouped/crossing-aware)
  and `zoned` (grouping force + concave-hull outlines + group labels, 2D first).
- Layout picker + "group by" facet selector (type / folder / tag) in the options
  pane. Force remains the default "explore" layout.
- Canvas hull overlay for the 2D renderer; 3D zoned hulls deferred/optional.

### Phase 2 — Query generalisation + filtering & lenses (Goal 3 + relevance)
- Backend: `GraphQuery` with type/tag/folder predicates; presets for
  neighborhood/full; close the metadata-in-neighborhood gap.
- Frontend: faceted filter panel (reuse `ListNotes` filter pattern) — additive
  across axes, OR within an axis; include/exclude chips.
- **Intent lenses** (one-click predicate+layout+emphasis bundles):
  Onboarding (`intro`+neighbors), Playbooks, Concept map (`concept`, zoned by
  folder), Decisions (`decision`+`policy`), This note's world (neighborhood),
  Everything (auto-aggregated). User-definable/pinnable later.
- **Focus + context** rendering (dim/shrink non-matches) + "hide non-matches"
  toggle. Focus note composes as a predicate.
- Persist active lens/filter per workspace.

### Phase 3 — Scale & graceful degradation (Goal 2)
- LOD label culling (by screen size / degree / zoom), replacing the fixed 220
  cap.
- **Auto-aggregation** past the threshold: cluster by facet (folder/type) into
  super-nodes with counts; expand a cluster on click. Client-side for ≤2K;
  server-side cap + aggregate mode for the rare large case (avoid shipping 10K
  live nodes).
- **Auto-disable 3D** above the threshold — fall back to 2D with a notice.
- Hard cap + "N more hidden" affordance on any expanded view.

### Phase 4 — Controls & interaction (Goal 4)
- One unified control scheme across 2D/3D (left-drag pan/orbit, scroll
  zoom-to-cursor, right-drag pan in 3D) + on-canvas legend/hint.
- Gentle auto-center on select: deterministic layouts place nodes immediately;
  force warm-starts from saved layout and the camera eases in — no settle-wait,
  no end-of-sim jump.
- Clean click model: click = select + gentle center, double-click = open,
  drag = reposition (persisted in 2D).

## Dependencies & sequencing

Phase 0 → 1, 2 (2 leans on the layout seam) → 3 (needs layouts + filtering) →
4 (polish across all). 1 and 2 can partly overlap after 0.

## Execution model (multi-agent)

The repo is **not** under git here, so agent worktree isolation is unavailable —
parallel agents must be partitioned by **non-overlapping file boundaries**. The
clean fault line is backend (Go) ∥ frontend (TS): no shared files.

- **Backend territory:** `internal/**`, `frontend/wailsjs/**` (generated
  bindings), HTTP handler/route.
- **Frontend territory:** `frontend/src/**`, `frontend/package.json`.
- The frontend converges on `GraphView3D.tsx`, so **only one frontend agent runs
  at a time** — frontend phases are sequential; the shared graph-core extraction
  (Phase 0) happens first so later phases touch mostly new files.

**Waves:**
- **Wave 1 (parallel):** Backend agent = full `GraphQuery` generalisation
  (Phases 0+2 backend, incl. closing the metadata-in-neighborhood gap) ∥
  Frontend agent = Phase 0 (delete dead Sigma view, extract shared graph core).
- **Wave 2 (sequential frontend, backend done):** layout engines (radial/zoned +
  hulls), then filtering/lenses + wire the new query.
- **Wave 3:** scale (LOD, auto-aggregation, auto-disable 3D).
- **Wave 4:** controls + gentle auto-center.

The main session integrates each wave (build + smoke test) before launching the
next.

## Open items (revisit during build)

- Exact aggregation threshold(s) and whether aggregation is client- or
  server-side for the mid-range (2–10K).
- Whether radial/zoned are offered in 3D or 2D-only.
- Lens set — refine names/predicates once seen against real workspaces.
- Angular ordering strategy for radial (by cluster vs. crossing minimisation).

## UX batch (user feedback) — DONE (build green: tsc + vite)

All in `frontend/src/ui/GraphView3D.tsx` unless noted. Every item below is
implemented; `tsc --noEmit` and `npm run build` both pass.

Summary of what shipped:
1. Force/radial no longer look wrong until a Zoned round-trip: `applyLayout()` and
   the spacing effect now re-assert their d3 forces on a post-commit `rAF` (the
   library rebuilds its sim on each `graphData` change and was clobbering them).
2. `RADIAL_RING_GAP` 120→70, `RADIAL_STRENGTH` 0.4→0.45 — tighter radial.
3. Delayed camera move gone: `onEngineStop` only persists now; framing happens once
   per load via double-`rAF` (whole-graph fit when nothing selected), `warmupTicks`
   20→60 so first paint is pre-settled, selection flights snappy (300ms). No reframe
   on slider/layout change.
4. Zone labels moved to the OUTER edge (centroid + outward normal × hull-radius),
   drawn in front (z=5, renderOrder 10, depthTest off) with a contrasting stroke;
   `groupColor` now uses a curated categorical palette (light/dark).
5. Legend shows zone swatches (per group, cap 8 + "+N more") in Zoned mode.
6. Zoom slider moved to a bottom-centre overlay in `.gm-graph-stage` (removed from
   the options pane).
7. Selected node/label colour `#e0603f` → `#ef4444` (`palette.ts`).
8. Lenses row removed from `GraphFilterPanel.tsx` (+ `onPickLens`/`handlePickLens`);
   `lenses.ts` kept for later.
9. Node size slider removed; `nodeSizeRef` fixed at 1.
10. Zoned disabled outside flat: `allowZoned={flat}` → Zoned button disabled in 3D.
11. Obsolete right-rail legend removed from `App.tsx` (kept the Notes/Links cards).

Still-open decision (unchanged): radial-force vs tuned-force winner; then delete
loser + dead `layout/radial.ts`.

<details><summary>Original batch spec (for reference)</summary>

1. **Bug: Zoned round-trip changes Force/Radial appearance; they look wrong before.** Root cause hypothesis: react-force-graph reinitializes its sim on each `graphData` change, clobbering our custom forces (`radial`/`x`/`y`, and possibly charge/link strengths) that the layout/spacing effects added to the *previous* sim. Fix: extract an `applyLayout()` and re-assert forces AFTER load via `requestAnimationFrame` (post-commit), plus keep the effect. Also for Force+flat, stop hard-pinning the restored saved layout — warm-start unpinned + always respread & re-persist (kills stale-layout).
2. **Radial pushes nodes too far.** Reduce `RADIAL_RING_GAP` 120→~70; consider lower charge influence in radial.
3. **Delayed camera move is distracting.** Remove `onEngineStop` framing (fires seconds late). Frame once right after load via rAF (ms=0 or very short); bump `warmupTicks` (20→~60) so first paint is pre-settled. Keep selection center but snappy (~300ms). Only frame on load + user select, never on slider/layout change.
4. **Zone labels.** Move to OUTER side of each zone (centroid + outward normal × (hullRadius+margin), outward = normalize(centroid) from origin; single-group → place above). Bring to top: z front (+), high `renderOrder`, `depthTest=false`. Add contrasting stroke. Use a curated categorical palette in `groupColor` (not hash-hue) for contrast.
5. **Legend shows zone values in Zoned mode.** When `layout==='zoned'`, render a swatch per group (from `hulls`, `groupColor`) instead of the note/link roles (cap ~8 + "…").
6. **Move zoom slider** out of the filter/options pane to a bottom/top overlay in `.gm-graph-stage` (interactive, pointerEvents auto).
7. **Selected node color** orange `#e0603f` → real red (e.g. `#ef4444`). Update `palette.ts` `nodeColor(selected)` + `labelColor(selected)`.
8. **Remove Lenses** from `GraphFilterPanel.tsx` (lens row) + `handlePickLens`/`onPickLens` wiring. Keep `lenses.ts` for later.
9. **Remove Node size slider** (state `nodeSize`/`setNodeSize` + UI). Keep `nodeSizeRef` fixed at 1.
10. **Disallow 3D + Zoned.** Pass `allowZoned={flat}` to `GraphFilterPanel`; disable the Zoned layout button when not flat.
11. **Remove obsolete right-pane graph legend** in `App.tsx` (the rail block: "GRAPH / A map of how your notes connect… / Current note / Connected / Other notes").

Still-open decision (unchanged): radial-force vs tuned-force winner; then delete loser + dead `layout/radial.ts`.

</details>
