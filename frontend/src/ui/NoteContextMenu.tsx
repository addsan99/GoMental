import {useEffect, useRef, useState} from 'react';
import type {MouseEvent, ReactNode} from 'react';

type ClipboardAction = 'cut' | 'copy' | 'paste';

type ClipboardState = {
  target: HTMLElement | null;
  selection: string;
  editable: boolean;
};

type NoteContextMenuProps = {
  children: ReactNode;
  enabled: boolean;
  zoom: number;
  canZoomIn: boolean;
  canZoomOut: boolean;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onResetZoom: () => void;
  onClipboardError: (message: string) => void;
};

type ContextMenuState = ClipboardState & {
  x: number;
  y: number;
};

const MENU_WIDTH = 232;
const MENU_HEIGHT = 262;

export function NoteContextMenu({
  children,
  enabled,
  zoom,
  canZoomIn,
  canZoomOut,
  onZoomIn,
  onZoomOut,
  onResetZoom,
  onClipboardError,
}: NoteContextMenuProps) {
  const [menu, setMenu] = useState<ContextMenuState | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const isMac = isMacPlatform();
  const modifier = isMac ? '⌘' : 'Ctrl+';

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

  const openMenu = (event: MouseEvent<HTMLDivElement>) => {
    if (!enabled) {
      return;
    }
    event.preventDefault();
    const target = editableTarget(event.target);
    const selection = selectedText(target);
    setMenu({
      target,
      selection,
      editable: Boolean(target && isEditable(target)),
      x: Math.max(8, Math.min(event.clientX, window.innerWidth - MENU_WIDTH - 8)),
      y: Math.max(8, Math.min(event.clientY, window.innerHeight - MENU_HEIGHT - 8)),
    });
  };

  const runClipboardAction = (action: ClipboardAction) => {
    if (!menu) {
      return;
    }
    void performClipboardAction(action, menu).catch(() => {
      onClipboardError(`Unable to ${action} using the system clipboard.`);
    });
    setMenu(null);
  };

  const runZoom = (action: () => void) => {
    action();
    setMenu(null);
  };

  const hasSelection = menu?.selection.length > 0;
  const canCut = Boolean(menu?.editable && hasSelection);
  const canCopy = Boolean(hasSelection);
  const canPaste = Boolean(menu?.editable);

  return (
    <div className="gm-note-context-surface" onContextMenu={openMenu}>
      {children}
      {menu && (
        <div
          ref={menuRef}
          className={isMac ? 'gm-note-context-menu gm-note-context-menu--mac' : 'gm-note-context-menu gm-note-context-menu--windows'}
          role="menu"
          aria-label="Note actions"
          style={{left: menu.x, top: menu.y}}
        >
          <MenuItem label="Cut" shortcut={`${modifier}X`} disabled={!canCut} onSelect={() => runClipboardAction('cut')} />
          <MenuItem label="Copy" shortcut={`${modifier}C`} disabled={!canCopy} onSelect={() => runClipboardAction('copy')} />
          <MenuItem label="Paste" shortcut={`${modifier}V`} disabled={!canPaste} onSelect={() => runClipboardAction('paste')} />
          <div className="gm-note-context-separator" role="separator" />
          <MenuItem label="Zoom In" shortcut={`${modifier}+`} disabled={!canZoomIn} onSelect={() => runZoom(onZoomIn)} />
          <MenuItem label="Zoom Out" shortcut={`${modifier}−`} disabled={!canZoomOut} onSelect={() => runZoom(onZoomOut)} />
          <MenuItem label={`Actual Size (${Math.round(zoom * 100)}%)`} shortcut={`${modifier}0`} disabled={zoom === 1} onSelect={() => runZoom(onResetZoom)} />
        </div>
      )}
    </div>
  );
}

function MenuItem({label, shortcut, disabled, onSelect}: {label: string; shortcut: string; disabled: boolean; onSelect: () => void}) {
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
      <span className="gm-note-context-shortcut">{shortcut}</span>
    </button>
  );
}

function isMacPlatform(): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(navigator.userAgent);
}

function editableTarget(target: EventTarget | null): HTMLElement | null {
  if (!(target instanceof Element)) {
    return null;
  }
  return target.closest<HTMLElement>('textarea, input, [contenteditable="true"]');
}

function isEditable(target: HTMLElement): boolean {
  if (target instanceof HTMLTextAreaElement) {
    return !target.disabled && !target.readOnly;
  }
  if (target instanceof HTMLInputElement) {
    return !target.disabled && !target.readOnly && !['button', 'checkbox', 'color', 'file', 'hidden', 'image', 'radio', 'range', 'reset', 'submit'].includes(target.type);
  }
  return target.isContentEditable;
}

function selectedText(target: HTMLElement | null): string {
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) {
    const start = target.selectionStart ?? 0;
    const end = target.selectionEnd ?? 0;
    return target.value.slice(start, end);
  }
  return window.getSelection()?.toString() ?? '';
}

async function performClipboardAction(action: ClipboardAction, state: ClipboardState): Promise<void> {
  const target = state.target;
  if ((action === 'copy' && !state.selection) || (action !== 'copy' && (!target || !state.editable))) {
    return;
  }
  target?.focus({preventScroll: true});
  if (document.execCommand(action)) {
    return;
  }

  if (action === 'copy') {
    await navigator.clipboard.writeText(state.selection);
    return;
  }

  if (action === 'paste') {
    const text = await navigator.clipboard.readText();
    if (!insertText(target, text)) {
      throw new Error('Paste target rejected the clipboard text');
    }
    return;
  }

  await navigator.clipboard.writeText(state.selection);
  if (action === 'cut' && !document.execCommand('delete')) {
    throw new Error('Cut target rejected deletion');
  }
}

function insertText(target: HTMLElement, text: string): boolean {
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) {
    const start = target.selectionStart ?? 0;
    const end = target.selectionEnd ?? start;
    target.setRangeText(text, start, end, 'end');
    target.dispatchEvent(new Event('input', {bubbles: true, composed: true}));
    return true;
  }
  return document.execCommand('insertText', false, text);
}

export default NoteContextMenu;
