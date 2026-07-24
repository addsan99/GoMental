// In-note "find" bar (Ctrl/Cmd+F) for the rendered read view. Highlights every
// occurrence of the query in the article and steps through them (Enter / ↓ = next,
// Shift+Enter / ↑ = previous), scrolling the current match to the centre.
//
// Matches are painted with the CSS Custom Highlight API (CSS.highlights + DOM
// Ranges) rather than by wrapping text in <mark> elements: that keeps React's DOM
// untouched (no reconciliation conflicts) and is cheap for large notes. WebView2
// is modern Chromium, so the API is available; if it were missing, navigation and
// scroll-to still work, only the visual highlight is skipped.
//
// The component is mounted only while the read view is showing, so it owns the
// Ctrl+F shortcut without stealing it from the source/MDX editors (which have
// their own search).
import {useCallback, useEffect, useRef, useState} from 'react';
import type {KeyboardEvent as ReactKeyboardEvent, RefObject} from 'react';

type FindBarProps = {
  // The scrolling element that contains the rendered article. Search is scoped to
  // the `.gm-article` inside it (so the bar's own text isn't matched), and scroll
  // math is done against this element.
  containerRef: RefObject<HTMLElement | null>;
  // Changes when the displayed note changes — re-runs the search against the new
  // content so an open find bar stays in sync after navigating.
  contentKey: string;
};

const ALL_HIGHLIGHT = 'gm-find';
const ACTIVE_HIGHLIGHT = 'gm-find-active';

type HighlightRegistry = {set: (name: string, highlight: unknown) => void; delete: (name: string) => void};
type HighlightCtor = new (...ranges: Range[]) => unknown;

function highlightRegistry(): HighlightRegistry | undefined {
  if (typeof CSS === 'undefined') {
    return undefined;
  }
  return (CSS as unknown as {highlights?: HighlightRegistry}).highlights;
}

function makeHighlight(ranges: Range[]): unknown {
  const ctor = (globalThis as unknown as {Highlight?: HighlightCtor}).Highlight;
  return ctor ? new ctor(...ranges) : undefined;
}

// Walk the text nodes under `root` and return a Range for every (case-insensitive)
// occurrence of `query`. Matches that span multiple text nodes (broken by inline
// markup) are not joined — this mirrors a simple browser find and covers the
// overwhelmingly common case of matches within a single run of text.
function collectRanges(root: HTMLElement, query: string): Range[] {
  const ranges: Range[] = [];
  if (!query) {
    return ranges;
  }
  const needle = query.toLowerCase();
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const value = node.nodeValue ?? '';
    if (value) {
      const haystack = value.toLowerCase();
      let from = 0;
      let at = haystack.indexOf(needle, from);
      while (at !== -1) {
        const range = document.createRange();
        range.setStart(node, at);
        range.setEnd(node, at + needle.length);
        ranges.push(range);
        from = at + needle.length;
        at = haystack.indexOf(needle, from);
      }
    }
    node = walker.nextNode();
  }
  return ranges;
}

export function FindBar({containerRef, contentKey}: FindBarProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [count, setCount] = useState(0);
  const [current, setCurrent] = useState(0); // 1-based position for display; 0 = none

  const inputRef = useRef<HTMLInputElement | null>(null);
  const rangesRef = useRef<Range[]>([]);
  const indexRef = useRef(0);
  const queryRef = useRef(query);
  queryRef.current = query;

  const clearHighlights = useCallback(() => {
    const registry = highlightRegistry();
    registry?.delete(ALL_HIGHLIGHT);
    registry?.delete(ACTIVE_HIGHLIGHT);
  }, []);

  const paint = useCallback((ranges: Range[], index: number) => {
    const registry = highlightRegistry();
    if (!registry) {
      return;
    }
    if (ranges.length === 0) {
      registry.delete(ALL_HIGHLIGHT);
      registry.delete(ACTIVE_HIGHLIGHT);
      return;
    }
    const all = makeHighlight(ranges);
    const active = makeHighlight([ranges[index]]);
    if (all) {
      registry.set(ALL_HIGHLIGHT, all);
    }
    if (active) {
      registry.set(ACTIVE_HIGHLIGHT, active);
    }
  }, []);

  const scrollTo = useCallback((range: Range) => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const containerRect = container.getBoundingClientRect();
    const rangeRect = range.getBoundingClientRect();
    if (rangeRect.height === 0 && rangeRect.width === 0) {
      return;
    }
    const target = rangeRect.top - containerRect.top + container.scrollTop - container.clientHeight / 2 + rangeRect.height / 2;
    container.scrollTo({top: Math.max(0, target), behavior: 'smooth'});
  }, [containerRef]);

  // Re-run the search for `value`. keepIndex preserves the current position where
  // possible (used when the note content refreshes); otherwise it jumps to the
  // first match — the expected behaviour when the query itself changes.
  const recompute = useCallback((value: string, keepIndex: boolean) => {
    const container = containerRef.current;
    const root = (container?.querySelector('.gm-article') as HTMLElement | null) ?? container ?? null;
    const ranges = root ? collectRanges(root, value) : [];
    rangesRef.current = ranges;
    setCount(ranges.length);
    if (ranges.length === 0) {
      indexRef.current = 0;
      setCurrent(0);
      paint(ranges, 0);
      return;
    }
    const index = keepIndex ? Math.min(indexRef.current, ranges.length - 1) : 0;
    indexRef.current = index;
    setCurrent(index + 1);
    paint(ranges, index);
    scrollTo(ranges[index]);
  }, [containerRef, paint, scrollTo]);

  const go = useCallback((delta: number) => {
    const ranges = rangesRef.current;
    if (ranges.length === 0) {
      return;
    }
    const next = (indexRef.current + delta + ranges.length) % ranges.length;
    indexRef.current = next;
    setCurrent(next + 1);
    paint(ranges, next);
    scrollTo(ranges[next]);
  }, [paint, scrollTo]);

  const close = useCallback(() => {
    setOpen(false);
    clearHighlights();
  }, [clearHighlights]);

  // Ctrl/Cmd+F opens the bar (and focuses/selects the field so a re-press lets the
  // user retype immediately). preventDefault suppresses the browser's own find.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'f') {
        event.preventDefault();
        setOpen(true);
        requestAnimationFrame(() => {
          inputRef.current?.focus();
          inputRef.current?.select();
        });
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // When the bar is open, (re)run the search after the note content settles — on
  // first open and whenever the displayed note changes.
  useEffect(() => {
    if (!open) {
      return;
    }
    const id = requestAnimationFrame(() => recompute(queryRef.current, true));
    return () => cancelAnimationFrame(id);
  }, [open, contentKey, recompute]);

  // Clear any painted highlights if the bar unmounts (e.g. leaving the read view).
  useEffect(() => clearHighlights, [clearHighlights]);

  const onQueryChange = useCallback((value: string) => {
    setQuery(value);
    recompute(value, false);
  }, [recompute]);

  const onKeyDown = useCallback((event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      go(event.shiftKey ? -1 : 1);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  }, [go, close]);

  if (!open) {
    return null;
  }

  const hasQuery = query.length > 0;
  const noResults = hasQuery && count === 0;

  return (
    <div className="gm-find-bar" role="search" onKeyDown={onKeyDown}>
      <input
        ref={inputRef}
        className="gm-find-input"
        type="text"
        placeholder="Find in note…"
        value={query}
        onChange={(event) => onQueryChange(event.target.value)}
        aria-label="Find in note"
      />
      <span className={noResults ? 'gm-find-count gm-find-none' : 'gm-find-count'}>
        {hasQuery ? (count > 0 ? `${current}/${count}` : 'No results') : ''}
      </span>
      <button type="button" className="gm-find-btn" onClick={() => go(-1)} disabled={count === 0} title="Previous (Shift+Enter)" aria-label="Previous match">↑</button>
      <button type="button" className="gm-find-btn" onClick={() => go(1)} disabled={count === 0} title="Next (Enter)" aria-label="Next match">↓</button>
      <button type="button" className="gm-find-btn" onClick={close} title="Close (Esc)" aria-label="Close find">✕</button>
    </div>
  );
}

export default FindBar;
