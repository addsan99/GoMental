import {useEffect, useRef, useState} from 'react';
import type {MouseEvent as ReactMouseEvent} from 'react';

export type NoteTreeAction = 'copyPath' | 'rename' | 'delete';

type MenuState = {id: string; x: number; y: number};

// Keeps the menu clear of the viewport edge when right-clicking near it. Only
// needs to be approximate — it is a clamp, not a layout measurement.
const MENU_WIDTH = 232;
const MENU_HEIGHT = 110;

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
    if (!onNoteAction) {
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

  const element = menu ? (
    <div
      ref={menuRef}
      className={/Mac|iPhone|iPad|iPod/i.test(navigator.userAgent) ? 'gm-note-context-menu gm-note-context-menu--mac' : 'gm-note-context-menu gm-note-context-menu--windows'}
      role="menu"
      aria-label="Note actions"
      style={{left: menu.x, top: menu.y}}
    >
      <NoteMenuItem label="Copy Full Path" disabled={false} onSelect={() => runAction('copyPath')} />
      <div className="gm-note-context-separator" role="separator" />
      <NoteMenuItem label="Rename…" disabled={!canMutate} onSelect={() => runAction('rename')} />
      <NoteMenuItem label="Delete…" disabled={!canMutate} onSelect={() => runAction('delete')} />
    </div>
  ) : null;

  return {openMenu, element};
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
