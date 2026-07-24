// Command palette modal. ⌘K/Ctrl-K toggles (wired in App). Filters loaded notes
// by title/path/tags; up/down select, Enter opens via onSelect, Esc closes.
import {useEffect, useMemo, useRef, useState} from 'react';
import type {application} from '../../wailsjs/go/models';
import {FileIcon, SearchIcon} from './icons';
import {noteTitle} from '../util';

type CommandPaletteProps = {
  open: boolean;
  notes: application.NoteSummaryDTO[];
  onClose: () => void;
  onSelect: (id: string) => void;
};

export function CommandPalette({open, notes, onClose, onSelect}: CommandPaletteProps) {
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (open) {
      setQuery('');
      setSelected(0);
      // Focus after the modal has mounted.
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
        onSelect(target.id);
      }
    } else if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
    }
  };

  return (
    <div className="gm-palette-scrim" onClick={onClose} role="presentation">
      <div className="gm-palette" onClick={(event) => event.stopPropagation()} role="dialog" aria-modal="true" aria-label="Command palette">
        <div className="gm-palette-input-row">
          <SearchIcon size={18} style={{color: 'var(--text-3)'}} />
          <input
            ref={inputRef}
            className="gm-palette-input"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setSelected(0);
            }}
            onKeyDown={handleKey}
            placeholder="Search notes, jump anywhere…"
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
                onClick={() => onSelect(note.id)}
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
          <span className="gm-palette-hint"><kbd className="gm-kbd small">↵</kbd>open</span>
        </div>
      </div>
    </div>
  );
}

export default CommandPalette;
