// Policy for flushing an open editor when the user navigates to another note.
//
// The decision runs from an effect cleanup, after the UI has already moved on,
// so it is kept pure and free of React state to stay testable.

export type EditSession = {
  /** The note the buffer below belongs to. */
  id: string;
  /** Live editor buffer. */
  draft: string;
  /** Last content known to be on disk. */
  savedContent: string;
  /** True while the editor pane is open. Read-mode viewing never flushes. */
  isEditing: boolean;
  /** False when the note or its workspace is read-only, or a conflict is open. */
  canSave: boolean;
};

export type FlushInput = {
  /** The note being navigated away from, captured when the session started. */
  leaving: string;
  session: EditSession;
  /** Set by delete/move/rename flows, whose source note is already gone. */
  suppressed: boolean;
};

/**
 * Reports whether the outgoing note has unsaved edits that should be written
 * before the editor is torn down.
 */
export function shouldFlushEdits(input: FlushInput): boolean {
  const {leaving, session, suppressed} = input;
  if (suppressed) {
    return false;
  }
  if (!leaving || !session.id) {
    return false;
  }
  // The buffer and the note it belongs to must agree. They can briefly disagree
  // while a newly selected note is still loading, and guessing there would write
  // one note's text into another.
  if (session.id !== leaving) {
    return false;
  }
  if (!session.isEditing || !session.canSave) {
    return false;
  }
  return session.draft !== session.savedContent;
}
