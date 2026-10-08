import {useEffect, useRef, useState} from 'react';
import type {MouseEvent as ReactMouseEvent} from 'react';

export type NoteTreeAction = 'copyPath' | 'rename' | 'delete';
export type FolderTreeAction = 'newChildFolder' | 'reveal' | 'copyPath';

type MenuState = {id: string; x: number; y: number};

// Keeps the menu clear of the viewport edge when right-clicking near it. Only
// needs to be approximate — it is a clamp, not a layout measurement.
const MENU_WIDTH = 232;
const ITEM_H = 28;
const SEPARATOR_H = 11;
const MENU_PADDING = 10;

function menuHeight(items: number, separators: number): number {
  return items * ITEM_H + separators * SEPARATOR_H + MENU_PADDING;
}

// Positioning and dismissal, shared by every right-click menu in the tree. The
// menus differ only in what they offer, so the behaviour that makes a menu feel
// native — where it lands, and everything that should close it — lives here
// once rather than being re-derived per menu and drifting apart.
function useContextMenu(height: number) {
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [menu, setMenu] = useState<MenuState | null>(null);

  // Dismiss on any interaction that would leave the menu stranded, including a
  // scroll of the list: the menu is viewport-positioned, so a scrolled row would
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
    event.preventDefault();
    event.stopPropagation();
    setMenu({
      id,
      x: Math.max(8, Math.min(event.clientX, window.innerWidth - MENU_WIDTH - 8)),
      y: Math.max(8, Math.min(event.clientY, window.innerHeight - height - 8)),
    });
  };

  return {menu, setMenu, menuRef, openMenu};
}

// Platform look: the menu itself is ours, so it at least follows the corner
// radius and translucency conventions of the host.
function menuClassName(): string {
  return isMac()
    ? 'gm-note-context-menu gm-note-context-menu--mac'
    : 'gm-note-context-menu gm-note-context-menu--windows';
}

function isMac(): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(navigator.userAgent);
}

// Shared right-click menu for a note row. The sidebar tree and the search
// results list both present notes, so they offer the same actions rather than
// making the available operations depend on whether a search is active.
export function useNoteActionMenu({
  onNoteAction,
  canMutate,
}: {
  onNoteAction?: (action: NoteTreeAction, id: string) => void;
  canMutate: boolean;
}) {
  const {menu, setMenu, menuRef, openMenu: open} = useContextMenu(menuHeight(3, 1));

  const openMenu = (event: ReactMouseEvent, id: string) => {
    if (!onNoteAction) {
      return;
    }
    open(event, id);
  };

  const runAction = (action: NoteTreeAction) => {
    if (!menu) {
      return;
    }
    const id = menu.id;
    setMenu(null);
    onNoteAction?.(action, id);
  };

  const element = menu ? (
    <div ref={menuRef} className={menuClassName()} role="menu" aria-label="Note actions" style={{left: menu.x, top: menu.y}}>
      <NoteMenuItem label="Copy Full Path" disabled={false} onSelect={() => runAction('copyPath')} />
      <div className="gm-note-context-separator" role="separator" />
      <NoteMenuItem label="Rename…" disabled={!canMutate} onSelect={() => runAction('rename')} />
      <NoteMenuItem label="Delete…" disabled={!canMutate} onSelect={() => runAction('delete')} />
    </div>
  ) : null;

  return {openMenu, element};
}

// Right-click menu for a folder row. A folder is a directory, not a note, so
// the actions are about the directory itself: adding to it, and reaching it
// from outside the app.
export function useFolderActionMenu({
  onFolderAction,
  canMutate,
}: {
  onFolderAction?: (action: FolderTreeAction, folder: string) => void;
  canMutate: boolean;
}) {
  const {menu, setMenu, menuRef, openMenu: open} = useContextMenu(menuHeight(3, 1));

  const openMenu = (event: ReactMouseEvent, folder: string) => {
    if (!onFolderAction) {
      return;
    }
    open(event, folder);
  };

  const runAction = (action: FolderTreeAction) => {
    if (!menu) {
      return;
    }
    const folder = menu.id;
    setMenu(null);
    onFolderAction?.(action, folder);
  };

  const element = menu ? (
    <div ref={menuRef} className={menuClassName()} role="menu" aria-label="Folder actions" style={{left: menu.x, top: menu.y}}>
      <NoteMenuItem label="New Child Folder…" disabled={!canMutate} onSelect={() => runAction('newChildFolder')} />
      <div className="gm-note-context-separator" role="separator" />
      <NoteMenuItem label={revealLabel()} disabled={false} onSelect={() => runAction('reveal')} />
      <NoteMenuItem label="Copy as Path" disabled={false} onSelect={() => runAction('copyPath')} />
    </div>
  ) : null;

  return {openMenu, element};
}

// Name the user's own file manager: "Reveal in Finder" on Windows reads as a
// different product, so the label follows the platform rather than ours.
function revealLabel(): string {
  if (isMac()) {
    return 'Reveal in Finder';
  }
  return /Windows/i.test(navigator.userAgent) ? 'Show in Explorer' : 'Open in File Manager';
}

function NoteMenuItem({label, disabled, onSelect}: {label: string; disabled: boolean; onSelect: () => void}) {
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
