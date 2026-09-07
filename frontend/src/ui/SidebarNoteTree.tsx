import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {CSSProperties, DragEvent, KeyboardEvent} from 'react';
import {ChevronIcon, FileIcon, FolderIcon, StarIcon} from './icons';
import {useNoteActionMenu} from './NoteActionMenu';
import type {NoteTreeAction} from './NoteActionMenu';
import {basename} from '../util';
import type {application} from '../../wailsjs/go/models';

export type TreeGroup = {
  name: string;
  notes: application.NoteSummaryDTO[];
};

export type {NoteTreeAction} from './NoteActionMenu';

type FolderRow = {kind: 'folder'; key: string; path: string; name: string; depth: number; count: number; open: boolean};
type FileRow = {kind: 'file'; key: string; note: application.NoteSummaryDTO; depth: number};
type FlatRow = FolderRow | FileRow;

// A folder in the note hierarchy. `notes` are the notes filed directly here;
// `total` counts the whole subtree, which is what a collapsed row has to report.
type FolderNode = {
  path: string;
  name: string;
  children: Map<string, FolderNode>;
  notes: application.NoteSummaryDTO[];
  total: number;
};

// Row height in px — must match `.gm-tree-row` height in App.css. The windowing
// maths below assumes every row is exactly this tall.
const ROW_H = 24;
// Below this many rows the list renders in normal flow; above it, only the
// visible window is mounted.
const VIRTUALIZE_THRESHOLD = 120;
const OVERSCAN = 8;

function emptyNode(path: string, name: string): FolderNode {
  return {path, name, children: new Map(), notes: [], total: 0};
}

// Walk to `path`, creating any folder along the way. Groups only exist for
// folders that directly hold a note, so intermediate folders have to be
// synthesized here or their descendants would render with no visible parent.
function ensureFolder(root: FolderNode, path: string): FolderNode {
  let node = root;
  let prefix = '';
  for (const segment of path.split('/')) {
    if (!segment) {
      continue;
    }
    prefix = prefix ? `${prefix}/${segment}` : segment;
    let child = node.children.get(segment);
    if (!child) {
      child = emptyNode(prefix, segment);
      node.children.set(segment, child);
    }
    node = child;
  }
  return node;
}

function countSubtree(node: FolderNode): number {
  let total = node.notes.length;
  for (const child of node.children.values()) {
    total += countSubtree(child);
  }
  node.total = total;
  return total;
}

function buildTree(groups: TreeGroup[]): FolderNode {
  const root = emptyNode('', '');
  for (const group of groups) {
    const node = group.name === 'Root' ? root : ensureFolder(root, group.name);
    node.notes = node.notes.concat(group.notes);
  }
  countSubtree(root);
  return root;
}

// flattenTree turns the folder tree into a flat, ordered row list so it can be
// windowed. A collapsed folder contributes only its own header row, hiding its
// whole subtree rather than just the notes filed directly in it.
function flattenTree(root: FolderNode, expanded: Record<string, boolean>): FlatRow[] {
  const rows: FlatRow[] = [];
  const walk = (node: FolderNode, depth: number) => {
    const folders = Array.from(node.children.values()).sort((a, b) => a.name.localeCompare(b.name));
    for (const child of folders) {
      const open = expanded[child.path] !== false;
      rows.push({kind: 'folder', key: `folder:${child.path}`, path: child.path, name: child.name, depth, count: child.total, open});
      if (open) {
        walk(child, depth + 1);
      }
    }
    for (const note of node.notes) {
      rows.push({kind: 'file', key: note.id, note, depth});
    }
  };
  walk(root, 0);
  return rows;
}

export default function SidebarNoteTree({
  tree,
  expanded,
  selectedID,
  activeTab,
  onSelectNote,
  onNavigateNote,
  onToggleFolder,
  onToggleFavorite,
  onMoveNote,
  onNoteAction,
  moveDisabled = false,
}: {
  tree: TreeGroup[];
  expanded: Record<string, boolean>;
  selectedID: string;
  activeTab: string;
  onSelectNote: (id: string) => void;
  onNavigateNote?: (id: string) => void;
  onToggleFolder: (name: string) => void;
  onToggleFavorite?: (id: string, favorite: boolean) => void;
  onMoveNote?: (id: string, folder: string) => void;
  onNoteAction?: (action: NoteTreeAction, id: string) => void;
  moveDisabled?: boolean;
}) {
  const rows = useMemo(() => flattenTree(buildTree(tree), expanded), [tree, expanded]);
  const navRef = useRef<HTMLElement | null>(null);
  const rowRefs = useRef(new Map<string, HTMLElement>());
  const [scrollTop, setScrollTop] = useState(0);
  const [viewport, setViewport] = useState(0);
  const [dragNoteID, setDragNoteID] = useState('');
  const [dropFolder, setDropFolder] = useState<string | null>(null);
  const [focusKey, setFocusKey] = useState('');
  // Set only by keyboard navigation, so an unrelated re-render never yanks
  // focus away from wherever the user actually put it.
  const focusPending = useRef(false);

  const virtualize = rows.length > VIRTUALIZE_THRESHOLD;

  const canMove = Boolean(onMoveNote) && !moveDisabled;
  // "Copy full path" is a read-only lookup, so the menu stays useful in
  // read-only workspaces even though the mutating entries are disabled there.
  const canMutate = Boolean(onNoteAction) && !moveDisabled;

  const {openMenu, element: contextMenu} = useNoteActionMenu({onNoteAction, canMutate});

  const handleDragStart = (event: DragEvent, id: string) => {
    if (!canMove) {
      event.preventDefault();
      return;
    }
    setDragNoteID(id);
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', id);
    event.dataTransfer.setData('application/x-gomental-note', id);
  };

  const noteIDFromDrop = (event: DragEvent): string => {
    return event.dataTransfer.getData('application/x-gomental-note') || event.dataTransfer.getData('text/plain') || dragNoteID;
  };

  const handleDrop = (event: DragEvent, folder: string) => {
    if (!canMove) {
      return;
    }
    const id = noteIDFromDrop(event);
    if (!id) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    setDropFolder(null);
    setDragNoteID('');
    onMoveNote?.(id, folder);
  };

  const handleDragOver = (event: DragEvent, folder: string) => {
    if (!canMove || !dragNoteID) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
    setDropFolder(folder);
  };

  const handleDragEnd = () => {
    setDragNoteID('');
    setDropFolder(null);
  };

  // Track the enclosing scroll container's offset/height only while virtualizing.
  useEffect(() => {
    if (!virtualize) {
      return;
    }
    const scroller = navRef.current?.parentElement;
    if (!scroller) {
      return;
    }
    const onScroll = () => setScrollTop(scroller.scrollTop);
    const measure = () => setViewport(scroller.clientHeight || 0);
    measure();
    setScrollTop(scroller.scrollTop);
    scroller.addEventListener('scroll', onScroll, {passive: true});
    const observer = new ResizeObserver(measure);
    observer.observe(scroller);
    return () => {
      scroller.removeEventListener('scroll', onScroll);
      observer.disconnect();
    };
  }, [virtualize]);

  // Move DOM focus onto the keyboard cursor. A windowed row may not be mounted
  // yet, in which case scrolling it into range re-renders and this runs again.
  useEffect(() => {
    if (!focusPending.current || !focusKey) {
      return;
    }
    const element = rowRefs.current.get(focusKey);
    if (element) {
      focusPending.current = false;
      element.focus();
      return;
    }
    const index = rows.findIndex((row) => row.key === focusKey);
    const scroller = navRef.current?.parentElement;
    if (index < 0 || !scroller) {
      focusPending.current = false;
      return;
    }
    const top = index * ROW_H;
    if (top < scroller.scrollTop) {
      scroller.scrollTop = top;
    } else if (top + ROW_H > scroller.scrollTop + scroller.clientHeight) {
      scroller.scrollTop = top + ROW_H - scroller.clientHeight;
    }
  }, [focusKey, rows, scrollTop]);

  const moveFocus = useCallback((key: string) => {
    if (!key) {
      return;
    }
    focusPending.current = true;
    setFocusKey(key);
  }, []);

  // Move the keyboard cursor and, when it lands on a note, open it. Selection
  // follows the cursor so the arrow keys browse notes rather than just shifting
  // focus; folder rows aren't notes, so those only take the cursor.
  const moveCursor = useCallback((key: string) => {
    if (!key) {
      return;
    }
    moveFocus(key);
    const row = rows.find((candidate) => candidate.key === key);
    if (row?.kind === 'file') {
      (onNavigateNote || onSelectNote)(row.note.id);
    }
  }, [moveFocus, onNavigateNote, onSelectNote, rows]);

  const handleKeyDown = (event: KeyboardEvent, index: number) => {
    const row = rows[index];
    if (!row) {
      return;
    }
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        moveCursor(rows[index + 1]?.key || row.key);
        return;
      case 'ArrowUp':
        event.preventDefault();
        moveCursor(rows[index - 1]?.key || row.key);
        return;
      case 'Home':
        event.preventDefault();
        moveCursor(rows[0]?.key || '');
        return;
      case 'End':
        event.preventDefault();
        moveCursor(rows[rows.length - 1]?.key || '');
        return;
      case 'ArrowRight':
        if (row.kind !== 'folder') {
          return;
        }
        event.preventDefault();
        if (row.open) {
          moveCursor(rows[index + 1]?.key || row.key);
        } else {
          onToggleFolder(row.path);
        }
        return;
      case 'ArrowLeft': {
        event.preventDefault();
        if (row.kind === 'folder' && row.open) {
          onToggleFolder(row.path);
          return;
        }
        // Jump to the enclosing folder: the nearest row above that is a folder
        // sitting one level shallower. Always a folder, so no note is opened.
        for (let i = index - 1; i >= 0; i -= 1) {
          const candidate = rows[i];
          if (candidate.kind === 'folder' && candidate.depth === row.depth - 1) {
            moveCursor(candidate.key);
            return;
          }
        }
        return;
      }
      default:
    }
  };

  const selectedKey = activeTab === 'note' && rows.some((row) => row.kind === 'file' && row.note.id === selectedID)
    ? selectedID
    : '';
  // Roving tabindex: exactly one row is reachable by Tab, and the arrow keys
  // move from there. Falls back to the selected note, then the first row.
  const tabKey = (focusKey && rows.some((row) => row.key === focusKey) ? focusKey : '') || selectedKey || rows[0]?.key || '';

  // WebKit doesn't focus a button when it's clicked, so on macOS the roving
  // tabindex below is unreachable by mouse: click a note and the arrow keys
  // still go to the document. Focus the row ourselves. Done on click rather
  // than mousedown so it can't interfere with starting a drag, and it's a no-op
  // for Enter/Space, where the row is already focused.
  const focusOnClick = (event: {currentTarget: HTMLElement}) => {
    event.currentTarget.focus();
  };

  const registerRow = (key: string) => (element: HTMLElement | null) => {
    if (element) {
      rowRefs.current.set(key, element);
    } else {
      rowRefs.current.delete(key);
    }
  };

  const renderRow = (row: FlatRow, index: number) => {
    const shared = {
      style: {'--tree-depth': row.depth} as CSSProperties,
      role: 'treeitem',
      tabIndex: row.key === tabKey ? 0 : -1,
      'aria-level': row.depth + 1,
      onKeyDown: (event: KeyboardEvent) => handleKeyDown(event, index),
      onFocus: () => setFocusKey(row.key),
    };
    if (row.kind === 'folder') {
      return (
        <button
          type="button"
          key={row.key}
          ref={registerRow(row.key)}
          {...shared}
          className={dropFolder === row.path ? 'gm-tree-row gm-tree-folder drop-target' : 'gm-tree-row gm-tree-folder'}
          aria-expanded={row.open}
          onClick={(event) => {
            focusOnClick(event);
            onToggleFolder(row.path);
          }}
          onDragOver={(event) => handleDragOver(event, row.path)}
          onDragLeave={() => setDropFolder((current) => current === row.path ? null : current)}
          onDrop={(event) => handleDrop(event, row.path)}
        >
          <span className={row.open ? 'gm-chevron open' : 'gm-chevron'}><ChevronIcon size={11} /></span>
          <FolderIcon size={13} className="gm-tree-folder-icon" />
          <span className="gm-tree-label">{row.name}</span>
          <span className="gm-tree-count">{row.count}</span>
        </button>
      );
    }
    const active = row.note.id === selectedID && activeTab === 'note';
    return (
      <button
        type="button"
        key={row.key}
        ref={registerRow(row.key)}
        {...shared}
        className={[
          'gm-tree-row gm-tree-file',
          active ? 'active' : '',
          dragNoteID === row.note.id ? 'dragging' : '',
        ].filter(Boolean).join(' ')}
        aria-selected={active}
        onClick={(event) => {
          focusOnClick(event);
          onSelectNote(row.note.id);
        }}
        onContextMenu={(event) => openMenu(event, row.note.id)}
        draggable={canMove}
        onDragStart={(event) => handleDragStart(event, row.note.id)}
        onDragEnd={handleDragEnd}
      >
        <span className="gm-chevron gm-chevron-leaf" aria-hidden="true" />
        <FileIcon size={13} className="gm-tree-file-icon" />
        <span className="gm-tree-label">{row.note.title || basename(row.note.id)}</span>
        <span
          role="button"
          tabIndex={-1}
          className={row.note.favorite ? 'gm-star gm-star-active' : 'gm-star'}
          title={row.note.favorite ? 'Remove from favorites' : 'Add to favorites'}
          aria-label={row.note.favorite ? 'Remove from favorites' : 'Add to favorites'}
          aria-pressed={Boolean(row.note.favorite)}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onToggleFavorite?.(row.note.id, !row.note.favorite);
          }}
          onKeyDown={(event) => {
            if (event.key !== 'Enter' && event.key !== ' ') {
              return;
            }
            event.preventDefault();
            event.stopPropagation();
            onToggleFavorite?.(row.note.id, !row.note.favorite);
          }}
        >
          <StarIcon size={13} filled={Boolean(row.note.favorite)} />
        </span>
      </button>
    );
  };

  if (!virtualize) {
    return (
      <nav
        className={dropFolder === '' ? 'gm-tree drop-root' : 'gm-tree'}
        role="tree"
        aria-label="Workspace notes"
        ref={navRef}
        onDragOver={(event) => handleDragOver(event, '')}
        onDragLeave={() => setDropFolder((current) => current === '' ? null : current)}
        onDrop={(event) => handleDrop(event, '')}
      >
        {rows.map(renderRow)}
        {contextMenu}
      </nav>
    );
  }

  const total = rows.length;
  const height = viewport || 600;
  const start = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN);
  const end = Math.min(total, Math.ceil((scrollTop + height) / ROW_H) + OVERSCAN);

  return (
    <nav
      className="gm-tree"
      role="tree"
      aria-label="Workspace notes"
      ref={navRef}
      style={{position: 'relative', height: total * ROW_H}}
      onDragOver={(event) => handleDragOver(event, '')}
      onDragLeave={() => setDropFolder((current) => current === '' ? null : current)}
      onDrop={(event) => handleDrop(event, '')}
    >
      <div style={{position: 'absolute', top: start * ROW_H, left: 0, right: 0, display: 'flex', flexDirection: 'column'}}>
        {rows.slice(start, end).map((row, offset) => renderRow(row, start + offset))}
      </div>
      {contextMenu}
    </nav>
  );
}
