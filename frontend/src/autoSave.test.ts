import {shouldFlushEdits} from './autoSave';
import type {EditSession} from './autoSave';

function session(overrides: Partial<EditSession> = {}): EditSession {
  return {
    id: 'notes/alpha',
    draft: 'edited body',
    savedContent: 'original body',
    isEditing: true,
    canSave: true,
    ...overrides,
  };
}

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(message);
  }
}

export const tests: Record<string, () => void> = {
  'flushes an edited note when navigating away': () => {
    assert(shouldFlushEdits({leaving: 'notes/alpha', session: session(), suppressed: false}), 'expected a flush');
  },

  'leaves an untouched note alone': () => {
    const unchanged = session({draft: 'original body'});
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: unchanged, suppressed: false}), 'clean buffer must not save');
  },

  'never flushes a note only being read': () => {
    const reading = session({isEditing: false});
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: reading, suppressed: false}), 'read mode must not save');
  },

  'never flushes a read-only note': () => {
    const locked = session({canSave: false});
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: locked, suppressed: false}), 'read-only must not save');
  },

  'respects suppression from delete and move flows': () => {
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: session(), suppressed: true}), 'suppressed must not save');
  },

  'will not write one note buffer into another': () => {
    const stale = session({id: 'notes/beta'});
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: stale, suppressed: false}), 'mismatched ids must not save');
  },

  'ignores an empty outgoing note': () => {
    assert(!shouldFlushEdits({leaving: '', session: session({id: ''}), suppressed: false}), 'no note means nothing to save');
  },

  'ignores a session with no note id': () => {
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: session({id: ''}), suppressed: false}), 'empty session id must not save');
  },

  'flushes a note emptied by the user': () => {
    const emptied = session({draft: ''});
    assert(shouldFlushEdits({leaving: 'notes/alpha', session: emptied, suppressed: false}), 'clearing a note is still an edit');
  },

  'flushes whitespace-only differences': () => {
    const spaced = session({draft: 'original body\n'});
    assert(shouldFlushEdits({leaving: 'notes/alpha', session: spaced, suppressed: false}), 'a trailing newline is still an edit');
  },

  'suppression beats every other signal': () => {
    const dirty = session({draft: 'lots of new text'});
    assert(!shouldFlushEdits({leaving: 'notes/alpha', session: dirty, suppressed: true}), 'suppression must win');
  },
};
