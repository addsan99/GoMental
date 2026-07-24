// Note picker modal for inserting a link to another note. ⌘L/Ctrl-L (or the
// "Link" button) opens it; picking a note inserts a resolved link at the cursor
// in the active editor. Mirrors CommandPalette's UI (reusing the .gm-palette-*
// styles) but returns the chosen note via onPick instead of navigating — kept
// separate so the ⌘K navigation path is untouched.
import {useEffect, useMemo, useRef, useState} from 'react';
import type {application} from '../../wailsjs/go/models';
import {FileIcon, LinkIcon} from './icons';
import {noteTitle} from '../util';

type LinkPickerProps = {
  open: boolean;
  notes: application.NoteSummaryDTO[];
  onClose: () => void;
  onPick: (note: application.NoteSummaryDTO) => void;
};

export function LinkPicker({open, notes, onClose, onPick}: LinkPickerProps) {
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (open) {
      setQuery('');
      setSelected(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  const results = useMemo(() => {
    const q = query.trim().toLocaleLowerCase();
    if (!q) {
      return notes;
    }
    return notes.filter((note) => {
      const haystack = `${noteTitle(note)} ${note.path || note.id} ${(note.tags || []).join(' ')}`.toLocaleLowerCase();
      return haystack.includes(q);
    });
  }, [notes, query]);

  const clampedSelected = Math.min(selected, Math.max(0, results.length - 1));

  useEffect(() => {
    const row = listRef.current?.querySelector<HTMLElement>(`[data-pal-index="${clampedSelected}"]`);
    row?.scrollIntoView({block: 'nearest'});
  }, [clampedSelected, results.length]);

  if (!open) {
    return null;
  }

  const handleKey = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setSelected((current) => Math.min(results.length - 1, current + 1));
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      setSelected((current) => Math.max(0, current - 1));
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const target = results[clampedSelected];
      if (target) {
        onPick(target);
      }
    } else if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
    }
  };

  return (
    <div className="gm-palette-scrim" onClick={onClose} role="presentation">
      <div className="gm-palette" onClick={(event) => event.stopPropagation()} role="dialog" aria-modal="true" aria-label="Link to a note">
        <div className="gm-palette-input-row">
          <LinkIcon size={18} style={{color: 'var(--text-3)'}} />
          <input
            ref={inputRef}
            className="gm-palette-input"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setSelected(0);
            }}
            onKeyDown={handleKey}
            placeholder="Link to a note…"
          />
          <kbd className="gm-kbd">ESC</kbd>
        </div>
        <div className="gm-palette-list scroll" ref={listRef}>
          {results.map((note, index) => {
            const isSelected = index === clampedSelected;
            return (
              <div
                key={note.id}
                data-pal-index={index}
                className={isSelected ? 'gm-palette-row selected' : 'gm-palette-row'}
                onClick={() => onPick(note)}
                onMouseEnter={() => setSelected(index)}
                role="option"
                aria-selected={isSelected}
              >
                <FileIcon size={16} style={{color: isSelected ? 'var(--accent-text)' : 'var(--text-3)'}} />
                <div className="gm-palette-row-text">
                  <div className="gm-palette-row-title">{noteTitle(note)}</div>
                  <div className="gm-palette-row-path">{note.path || note.id}</div>
                </div>
                {isSelected && <kbd className="gm-kbd">↵</kbd>}
              </div>
            );
          })}
          {results.length === 0 && <div className="gm-palette-empty">Nothing found.</div>}
        </div>
        <div className="gm-palette-footer">
          <span className="gm-palette-hint"><kbd className="gm-kbd small">↑↓</kbd>navigate</span>
          <span className="gm-palette-hint"><kbd className="gm-kbd small">↵</kbd>insert link</span>
        </div>
      </div>
    </div>
  );
}

export default LinkPicker;
