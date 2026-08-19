import {useEffect, useMemo, useRef, useState} from 'react';
import type {CSSProperties, DragEvent, MouseEvent as ReactMouseEvent} from 'react';
import {ChevronIcon, FileIcon, FolderIcon, StarIcon} from './icons';
import {basename} from '../util';
import type {application} from '../../wailsjs/go/models';

export type TreeGroup = {
  name: string;
  depth: number;
  notes: application.NoteSummaryDTO[];
};

export type NoteTreeAction = 'copy' | 'copyPath' | 'rename' | 'delete';

type FolderRow = {kind: 'folder'; key: string; name: string; depth: number; count: number; open: boolean};
type FileRow = {kind: 'file'; key: string; note: application.NoteSummaryDTO; depth: number};
type FlatRow = FolderRow | FileRow;

type MenuState = {id: string; x: number; y: number};

// Keeps the menu clear of the viewport edge when right-clicking near it. Only
// needs to be approximate — it is a clamp, not a layout measurement.
const MENU_WIDTH = 232;
const MENU_HEIGHT = 150;

// Row height in px — must match `.gm-tree-folder` / `.gm-tree-file` height in App.css.
const ROW_H = 33;
// Below this many rows the list renders in normal flow (pixel-identical to the
// old markup); above it, only the visible window is mounted.
const VIRTUALIZE_THRESHOLD = 120;
const OVERSCAN = 8;

// flattenTree turns the collapsible folder tree into a flat, ordered row list so
// it can be windowed. Collapsed folders contribute only their header row.
function flattenTree(tree: TreeGroup[], expanded: Record<string, boolean>): FlatRow[] {
  const rows: FlatRow[] = [];
  for (const group of tree) {
    const isRoot = group.name === 'Root';
    const open = expanded[group.name] !== false;
    if (!isRoot) {
      rows.push({kind: 'folder', key: `folder:${group.name}`, name: group.name, depth: group.depth - 1, count: group.notes.length, open});
    }
    if (open) {
      for (const note of group.notes) {
        rows.push({kind: 'file', key: note.id, note, depth: isRoot ? 0 : group.depth});
      }
    }
  }
  return rows;
}

export default function SidebarNoteTree({
  tree,
  expanded,
  selectedID,
  activeTab,
  onSelectNote,
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
  onToggleFolder: (name: string) => void;
  onToggleFavorite?: (id: string, favorite: boolean) => void;
  onMoveNote?: (id: string, folder: string) => void;
  onNoteAction?: (action: NoteTreeAction, id: string) => void;
  moveDisabled?: boolean;
}) {
  const rows = useMemo(() => flattenTree(tree, expanded), [tree, expanded]);
  const navRef = useRef<HTMLElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewport, setViewport] = useState(0);
  const [dragNoteID, setDragNoteID] = useState('');
  const [dropFolder, setDropFolder] = useState<string | null>(null);
  const [menu, setMenu] = useState<MenuState | null>(null);

  const virtualize = rows.length > VIRTUALIZE_THRESHOLD;

  const canMove = Boolean(onMoveNote) && !moveDisabled;
  // "Copy full path" is a read-only lookup, so the menu stays useful in
  // read-only workspaces even though the mutating entries are disabled there.
  const canAct = Boolean(onNoteAction);
  const canMutate = canAct && !moveDisabled;

  // Dismiss on any interaction that would leave the menu stranded, including a
  // scroll of the tree: the menu is viewport-positioned, so a scrolled row would
  // otherwise drift away from it.
  useEffect(() => {
    if (!menu) {
      return;
    }
    const close = (event: Event) => {
      if (event.type === 'pointerdown' && menuRef.current?.contains(event.target as Node)) {
        return;
      }
      setMenu(null);
    };
    window.addEventListener('pointerdown', close);
    window.addEventListener('resize', close);
    window.addEventListener('scroll', close, true);
    window.addEventListener('keydown', close);
    return () => {
      window.removeEventListener('pointerdown', close);
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
      window.removeEventListener('keydown', close);
    };
  }, [menu]);

  const openMenu = (event: ReactMouseEvent, id: string) => {
    if (!canAct) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    setMenu({
      id,
      x: Math.max(8, Math.min(event.clientX, window.innerWidth - MENU_WIDTH - 8)),
      y: Math.max(8, Math.min(event.clientY, window.innerHeight - MENU_HEIGHT - 8)),
    });
  };

  const runAction = (action: NoteTreeAction) => {
    if (!menu) {
      return;
    }
    const id = menu.id;
    setMenu(null);
    onNoteAction?.(action, id);
  };

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

  const renderRow = (row: FlatRow) => {
    if (row.kind === 'folder') {
      return (
        <button
          type="button"
          key={row.key}
          className={dropFolder === row.name ? 'gm-tree-folder drop-target' : 'gm-tree-folder'}
          style={{'--tree-depth': row.depth} as CSSProperties}
          onClick={() => onToggleFolder(row.name)}
          onDragOver={(event) => handleDragOver(event, row.name)}
          onDragLeave={() => setDropFolder((current) => current === row.name ? null : current)}
          onDrop={(event) => handleDrop(event, row.name)}
        >
          <span className={row.open ? 'gm-chevron open' : 'gm-chevron'}><ChevronIcon size={13} /></span>
          <FolderIcon size={16} />
          <span className="gm-tree-folder-label">{basename(row.name)}</span>
          <span className="gm-tree-count">{row.count}</span>
        </button>
      );
    }
    const active = row.note.id === selectedID && activeTab === 'note';
    return (
      <button
        type="button"
        key={row.key}
        className={[
          active ? 'gm-tree-file active' : 'gm-tree-file',
          dragNoteID === row.note.id ? 'dragging' : '',
        ].filter(Boolean).join(' ')}
        style={{'--tree-depth': row.depth} as CSSProperties}
        onClick={() => onSelectNote(row.note.id)}
        onContextMenu={(event) => openMenu(event, row.note.id)}
        draggable={canMove}
        onDragStart={(event) => handleDragStart(event, row.note.id)}
        onDragEnd={handleDragEnd}
      >
        <FileIcon size={15} className="gm-tree-file-icon" />
        <span className="gm-tree-file-label">{row.note.title || basename(row.note.id)}</span>
        <span
          role="button"
          tabIndex={0}
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
          <StarIcon size={14} filled={Boolean(row.note.favorite)} />
        </span>
      </button>
    );
  };

  const contextMenu = menu ? (
    <div
      ref={menuRef}
      className={/Mac|iPhone|iPad|iPod/i.test(navigator.userAgent) ? 'gm-note-context-menu gm-note-context-menu--mac' : 'gm-note-context-menu gm-note-context-menu--windows'}
      role="menu"
      aria-label="Note actions"
      style={{left: menu.x, top: menu.y}}
    >
      <TreeMenuItem label="Copy" disabled={!canMutate} onSelect={() => runAction('copy')} />
      <TreeMenuItem label="Copy Full Path" disabled={false} onSelect={() => runAction('copyPath')} />
      <div className="gm-note-context-separator" role="separator" />
      <TreeMenuItem label="Rename…" disabled={!canMutate} onSelect={() => runAction('rename')} />
      <TreeMenuItem label="Delete…" disabled={!canMutate} onSelect={() => runAction('delete')} />
    </div>
  ) : null;

  if (!virtualize) {
    return (
      <nav
        className={dropFolder === '' ? 'gm-tree drop-root' : 'gm-tree'}
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
  const visible = rows.slice(start, end);

  return (
    <nav
      className="gm-tree"
      aria-label="Workspace notes"
      ref={navRef}
      style={{position: 'relative', height: total * ROW_H}}
      onDragOver={(event) => handleDragOver(event, '')}
      onDragLeave={() => setDropFolder((current) => current === '' ? null : current)}
      onDrop={(event) => handleDrop(event, '')}
    >
      <div style={{position: 'absolute', top: start * ROW_H, left: 0, right: 0, display: 'flex', flexDirection: 'column'}}>
        {visible.map(renderRow)}
      </div>
      {contextMenu}
    </nav>
  );
}

function TreeMenuItem({label, disabled, onSelect}: {label: string; disabled: boolean; onSelect: () => void}) {
  return (
    <button
      type="button"
      className="gm-note-context-item"
      role="menuitem"
      disabled={disabled}
      onMouseDown={(event) => event.preventDefault()}
      onClick={onSelect}
    >
      <span>{label}</span>
    </button>
  );
}
