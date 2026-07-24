# Handoff: GoMental — Wiki / Notes Desktop App Redesign

## Overview
GoMental is a local-first Markdown knowledge base (a "wiki" over a folder of `.md`
files on disk). This package is a full visual + interaction redesign of the app:
a sleek, modern, three-pane reader/editor with global search, a command palette,
a connections graph, backlinks, and **light + dark themes**.

The target runtime is a **Go application with a [Wails](https://wails.io) frontend**
(Go backend + a webview-hosted web UI). **The frontend is React.** The redesign is
delivered as an HTML prototype and should be re-implemented as **React components**
inside the Wails frontend, using that project's existing patterns.

## About the Design Files
The file in this bundle (`GoMental.dc.html`) is a **design reference created in
HTML** — a working prototype that demonstrates the intended look, layout, and
behavior. **It is not production code to copy verbatim.**

- It is authored as a "Design Component" and depends on a proprietary runtime
  (`support.js`) to run as-is, so treat it as a *spec you read and run for
  reference*, not a drop-in module.
- The task is to **recreate these designs as React components in the Wails
  frontend** using that project's established patterns (its component conventions,
  its CSS approach, its state layer) and to wire the UI actions to **Go backend
  methods** exposed through the generated Wails bindings
  (`import { ListNotes } from '../wailsjs/go/main/App'`).
- All layout, tokens, copy, and interactions in this README are the source of
  truth. Where the prototype and this README ever disagree, follow the README.

## Fidelity
**High-fidelity (hifi).** Final colors, typography, spacing, radii, shadows, and
interactions are all specified below. Recreate the UI pixel-accurately, then swap
the mocked data/handlers for real Go-backed calls.

---

## Design Tokens

All values are CSS custom properties driven by two attributes on the root element:
`data-theme="light|dark"` and (optional) `data-accent="iris|emerald|amber|blue"`.
Default accent is **iris**. A `data-face="serif|sans"` attribute swaps the reading
typeface.

### Typography
| Role | Family | Notes |
|---|---|---|
| UI / chrome | **Hanken Grotesk** (`--font-ui`) | 400/500/600/700 |
| Reading (note body, titles, tables) | **Newsreader** serif (`--font-read`) | 400/500/600 + italic; opsz 6–72 |
| Code / mono / paths | **JetBrains Mono** (`--font-mono`) | 400/500 |

Load from Google Fonts:
`Hanken+Grotesk:wght@400;500;600;700`, `Newsreader:ital,opsz,wght@0,6..72,400;0,6..72,500;0,6..72,600;1,6..72,400;1,6..72,500`, `JetBrains+Mono:wght@400;500`.

Key type ramps (all reading text uses `--font-read`):
- Note H1 title: `600 42px/1.08`, letter-spacing `-0.6px`
- Note summary/lead: `400 19px/1.6`, color `--text-2`
- Section H2: `600 27px/1.2`, letter-spacing `-0.3px`
- Body paragraph: `400 17px/1.72`
- Step text: `400 17px/1.62`; sub-item `400 16px/1.55`
- Table cell: `400/500 15px/1.5` (ingredient col 400, amount col 500 + tabular-nums)
- UI labels: `500–600 11–14px` Hanken Grotesk; section rail headers `600 11px` uppercase, letter-spacing `.09em`
- Mono paths/kbd: `400–500 10.5–13.5px` JetBrains Mono

### Color tokens

**Light theme**
```
--bg:#f6f4f0   --surface:#fffefb   --surface-2:#f0ede7   --surface-3:#e9e5dd
--border:#e6e2d9   --border-strong:#d6d1c5
--text:#26232b   --text-2:#6b6760   --text-3:#9c988e
--accent:#5b53f5 --accent-hover:#4a41ea --accent-press:#3d34d6
--accent-text:#4b42db --accent-soft:#ecebff --accent-soft-2:#e2e0ff --accent-soft-line:#d3d0ff
--accent-on:#ffffff
--mark:rgba(240,167,58,.32)  (search highlight)
--good:#2f9e6f  --good-soft:#e4f5ec
```

**Dark theme**
```
--bg:#0e0e12   --surface:#16161c   --surface-2:#1e1e26   --surface-3:#26262f
--border:#26262e   --border-strong:#34343f
--text:#eceaf2   --text-2:#a3a0ad   --text-3:#6d6a78
--accent:#8b83ff --accent-hover:#9c95ff --accent-press:#7a71f5
--accent-text:#a9a2ff --accent-soft:rgba(139,131,255,.16) --accent-soft-2:rgba(139,131,255,.24) --accent-soft-line:rgba(139,131,255,.4)
--accent-on:#0e0e12
--mark:rgba(242,180,90,.28)  --good:#4fd0a2  --good-soft:rgba(79,208,162,.14)
```

**Alternate accents** (override `--accent*` only; both themes provided):
- emerald light `#0f9d6b` / dark `#3ddba1`
- amber light `#e0862a` / dark `#f2b45a`
- blue light `#2a6ff5` / dark `#5b95ff`

### Shadows
```
--shadow-sm: 0 1px 2px rgba(38,35,30,.05)                     [dark: 0 1px 2px rgba(0,0,0,.4)]
--shadow-md: 0 6px 20px -6px rgba(38,35,30,.14), 0 2px 6px rgba(38,35,30,.06)
--shadow-lg: 0 24px 60px -16px rgba(30,27,40,.30), 0 8px 20px rgba(30,27,40,.12)  [modals]
```

### Radii & spacing
- Radii: inputs/buttons `8–9px`, cards/tables `10–12px`, modal `16px`, pills/chips `9999px`.
- Spacing on a 4px grid. Common paddings: article `36px 40px 120px`, panel `16–20px`, rows `9–11px`.
- Row heights: sidebar nav row `33px`, buttons `30–36px`, top header `56px`.

---

## Layout

Root is a full-viewport grid: `grid-template-rows: 56px 1fr` (header, then body).
The body is a **three-column grid**: `290px  minmax(0,1fr)  300px`
(left sidebar · main · right rail). All three columns are independent scroll
areas; the header is fixed.

### Header (56px, `--surface`, bottom `1px --border`)
- **Left:** app mark (30px rounded-9px accent tile with a small "connected nodes"
  glyph in `--accent-on`), then `GoMental` (`700 15px` UI) with the workspace path
  under it in mono `10.5px --text-3` (`F:\dev\Go\gomindy\recipes`).
- **Center-left:** command-palette trigger button — pill, `--surface-2`, `1px --border`,
  radius 9, height 36, min-width 230; search icon + "Jump to anything…" + a `⌘K`
  `<kbd>`. Opens the palette.
- **Right:** `Rebuild index` button (surface, `1px --border-strong`) whose refresh
  icon spins while rebuilding; and a theme-toggle icon button (sun in dark / moon
  in light).

### Left sidebar (290px, `--surface`, right `1px --border`)
1. **Header row:** "NOTES" (uppercase `600 11px` `.09em`) + count in mono; a primary
   **New** button (accent fill, `+` icon) and an icon-only **Import** button.
2. **Search input:** height 37, `--surface-2`, `1px --border`, radius 9, search icon
   left, clear (×) button appears right when non-empty. Focus ring:
   `border-color --accent; box-shadow 0 0 0 3px --accent-soft`.
3. **Scroll body — two mutually exclusive states:**
   - *Empty query →* **folder tree**. Rows: chevron (folders, rotates 90°→open) +
     folder/file icon + label (+ descendant count for folders, mono, right-aligned).
     Depth indent = `9 + depth*15`px left padding. Active file row:
     `background --accent-soft; color --text; font-weight 600; inset box-shadow 2px 0 --accent`
     (an accent spine). Hover on inactive rows: `--surface-2`.
   - *Non-empty query →* **search results**: an "N results" label, then result cards
     (title UI 600 13px + faint mono path; 2-line clamped snippet with the matched
     term wrapped in `<mark>` using `--mark`). Empty → "No notes match ‘…’.".
4. **Footer strip:** green status dot (with `--good-soft` ring) + "Search index ready"
   / "Rebuilding index…".

### Main column
- **Sub-header (`--surface`, bottom border):** breadcrumb (`GoMental / [folder] / file`,
  mono-ish UI 12px, `--text-3`), and on the right a **Source/Preview** toggle button
  and the **Save** primary button (shows a check + "Saved" for ~1.5s after save).
  Below: **Note** / **Graph** tabs (active = `--accent-text` text + 2px `--accent`
  underline), and on the far right meta text: "Edited {relative}" (clock icon) and
  "{n} words · {n} min read".
- **Three view modes** inside the main column:
  - **Note (rendered):** centered article, max-width 748px. Tag chips
    (accent-soft pills, `#tag`), serif H1, serif lead paragraph, then a block
    renderer: **H2** (with `data-anchor` for scroll-spy), **paragraphs**, **tables**
    (rounded bordered card, uppercase `--text-3` header row on `--surface-2`, row
    top-borders, amount column right-aligned tabular-nums), **numbered steps**
    (26px accent-soft numbered circle + serif text, optional bullet sub-items),
    **bulleted lists**, and **callouts** (accent-soft box, lightbulb icon, uppercase
    accent title + serif body). Inline Markdown supported: `**bold**`, `` `code` ``
    (mono chip), and `[[wiki links]]` (accent underline; click navigates to that note).
  - **Source:** a "code editor" card — mono `13.5px/1.75` `<textarea>` prefilled with
    the note's Markdown (frontmatter + body), traffic-light title bar showing
    `<file>.md`. Edits are held as per-note drafts.
  - **Graph:** dotted-grid canvas with an SVG force-style layout — the current note
    is centered (accent fill + soft halo), other notes ring around it, edges drawn
    from wiki-links (edges touching the current note are accent + thicker). Every
    node is clickable → opens that note. Labels under nodes (UI 13px).

### Right rail (300px, `--surface`, left `1px --border`, scrolls)
- **Note mode:**
  - **On this page** — outline of the note's H2s; each is a hover-highlighting link
    with a left `2px --border` guide; click smooth-scrolls the article to that heading.
  - **Details** — label/value rows: Path, Format (Markdown), Modified, Words, Links,
    Backlinks (values in mono `--text-2`, right-aligned).
  - **Linked notes** — outgoing wiki-links (count pill); each row = link icon + title,
    click opens.
  - **Backlinks** — notes that link *to* this one (count pill): bordered cards with
    source title + 2-line context snippet, click opens. Empty → dashed-border empty
    state with a link icon and "No other note links here yet."
- **Graph mode:** a legend (Current note / Connected / Other notes) and two stat
  cards (Notes count, Links count).

### Command palette (modal overlay)
- Scrim `rgba(20,18,28,.44)`, centered card `min(600px,92vw)` at `12vh` from top,
  `--surface`, radius 16, `--shadow-lg`; enter animation `gm-pop` (`.16s cubic-bezier(.2,.8,.2,1)`).
- Search field (16px), `ESC` kbd; result rows (file icon + title + mono path); the
  selected row is `--accent-soft` and shows a `↵` kbd. Footer legend: ↑↓ navigate, ↵ open.
- Keyboard: **⌘K / Ctrl-K** toggles; **↑/↓** move selection; **Enter** opens the
  selected note; **Esc** closes.

### Toast
- Bottom-center pill, `--text` bg / `--bg` text, `--shadow-lg`, green check icon,
  auto-dismiss ~1.9s. Used for New / Import / Save / Rebuild confirmations.

---

## Interactions & Behavior
- **Note selection** (tree, search result, wiki-link, backlink, linked-note, graph
  node, palette): sets active note, forces Note mode (exits Source/Graph), and resets
  the article scroll to top.
- **Search** filters live as the user types over title + tags + summary + full
  Markdown; snippet is centered on the first match with the term `<mark>`-highlighted.
- **Folder collapse**: chevron rotates `.15s`; children hidden when collapsed.
- **Source toggle** swaps rendered ↔ raw Markdown editor (drafts kept per note in state).
- **Save**: button flashes "Saved ✓" 1.5s + toast (wire to disk write).
- **Rebuild index**: refresh icon spins ~1.3s, footer shows "Rebuilding index…",
  then toast "Index rebuilt · N notes" (wire to backend reindex).
- **Theme toggle**: flips `data-theme`; **persisted to `localStorage` key `gm-theme`**.
- **Hover states** (all `--surface-2`-ish washes): buttons, nav rows, result cards,
  outline links, backlink cards, palette rows. Transitions `.12–.15s`.
- **Scroll-spy**: outline links use `data-anchor` (slug of heading) to locate the H2
  and smooth-scroll the article container.

## State Management
Minimal client state — map each of these to `useState`/`useReducer` (or the app's
store) in React (see the prototype's logic class for the reference shape):
- `theme` (`light|dark`, persisted), `accent`, `readingFace`
- `query` (sidebar search), `activeId` (current note), `mode` (`note|graph`),
  `raw` (source view on/off)
- `expanded` (map of folder → open/closed)
- `paletteOpen`, `paletteQuery`, `palSel` (highlighted palette index)
- `rebuilding`, `savedFlash`, `toast` (transient UI), `rawDrafts` (per-note editor text)

### Data model (per note)
`{ id, title, file (display name), folder|null, path (e.g. "recipes/glazed-salmon.md"),
updated (relative string), tags[], summary, blocks[], links[] }`.
`blocks[]` items: `{t:'h2'|'p'|'table'|'steps'|'list'|'callout', …}`.
Backlinks and the graph are **derived** from every note's `links[]` (extracted from
`[[wiki link]]` syntax → slug). Word count and read-time are derived from content.

## Wails / Go integration (React frontend)
Replace the prototype's in-memory `notes` object and mock handlers with calls to Go
backend methods via the **generated TypeScript bindings** Wails emits under
`frontend/wailsjs/go/main/App` (e.g. `import { Search, ReadNote, SaveNote } from '../wailsjs/go/main/App'`).
Bindings return Promises — call them from `useEffect` / event handlers and hold the
result in state. For push events from Go (e.g. external file changes), subscribe with
`EventsOn(...)` from `wailsjs/runtime`. Suggested backend surface:
- `ListNotes() []NoteMeta` — walk the workspace dir, return tree + metadata.
- `ReadNote(path) NoteContent` — raw Markdown (+ parsed frontmatter).
- `SaveNote(path, markdown) error` — write to disk.
- `Search(query, pathFilter, limit) []Hit` — full-text (backend already has an index;
  the "Rebuild index" action maps to a reindex call).
- `ImportNote(...)` — open a native file dialog (`runtime.OpenFileDialog`) and copy in.
- `Rebuild() Stats`, `OpenWorkspace()` (native folder dialog), `GetGraph() {nodes,edges}`.
Render Markdown either backend-side (goldmark → HTML) or client-side; the prototype
renders client-side from a small block model. In React, a `react-markdown` pipeline
with a `remark` plugin for `[[wikilinks]]` (or a small custom renderer) is the
natural fit — but preserve the inline `**bold**` / `` `code` `` / `[[wikilink]]`
handling and the table/steps/callout styling above. Suggested component breakdown:
`AppShell` (grid + theme attr), `Header`, `Sidebar` (`SearchBox`, `NoteTree`,
`SearchResults`), `NoteView` (`Article` block renderer, `SourceEditor`), `GraphView`,
`RightRail` (`Outline`, `Details`, `LinkedNotes`, `Backlinks`), `CommandPalette`,
`Toast`.

Because Wails hosts a system webview, the fonts, CSS custom properties, fl/grid, and
SVG here all work unchanged; no polyfills needed. Keep the theme attribute on the
root element so `data-theme` cascades to every token.

## Assets
- **App logo/mark:** inline SVG in the header (rounded accent tile + "connected
  nodes" glyph). No external image — reproduce as an SVG/icon component.
- **All other icons:** inline stroke SVGs (search, plus, download/import, refresh,
  sun/moon, file, folder, chevron, clock, code brackets, save/disk, link, lightbulb,
  graph). ~1.8–2px stroke, rounded joins, `currentColor`. Swap for the codebase's
  icon set (e.g. Lucide/Phosphor) at matching weight if preferred.
- **Fonts:** Google Fonts (Hanken Grotesk, Newsreader, JetBrains Mono) — self-host in
  the Wails bundle for offline/desktop use.
- No raster images.

## Files
- `GoMental.dc.html` — the high-fidelity prototype (all screens, states, and the
  reference logic/state model). Open it to interact with search, the ⌘K palette,
  theme toggle, graph, source view, and navigation. Sample content is a set of
  recipe notes matching the original app; replace with the real workspace at runtime.
- `screenshots/` — reference captures:
  `01-note-light.png` (note reader, light), `02-note-dark.png` (note reader, dark),
  `03-graph-dark.png` (connections graph), `04-command-palette.png` (⌘K palette).
