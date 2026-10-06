// Policy for what to do when a `note:updated` event arrives for the open note.
//
// The event has three very different causes and they must not be confused:
//   - our own save echoing back through the watcher,
//   - an external change (another editor, a git pull, the MCP server) while we
//     have nothing unsaved,
//   - an external change while we *do* have unsaved edits.
//
// Only the last one is a conflict. Deciding this inline in the event handler
// made it untestable and easy to get wrong, so the rule lives here.

export type RefreshDecision = 'ignore' | 'echo' | 'refresh' | 'conflict';

export type RefreshInput = {
  /** Note the event is about. */
  incomingID: string;
  /** Note currently on screen. Read from a ref: the event can arrive while the
   *  call that caused it is still in flight, so a closed-over value goes stale. */
  selectedID: string;
  /** Version token on the event, '' when absent. */
  incomingVersion: string;
  /** Version token of what we currently have loaded. */
  currentVersion: string;
  /** Content on the event. */
  incomingContent: string;
  /** What the editor is showing. */
  draft: string;
  /** What we last read from or wrote to disk. */
  savedContent: string;
};

export function decideRefresh(input: RefreshInput): RefreshDecision {
  if (!input.incomingID || input.incomingID !== input.selectedID) {
    return 'ignore';
  }
  // Same version as what we hold: nothing new on disk.
  if (input.incomingVersion && input.incomingVersion === input.currentVersion) {
    return 'ignore';
  }
  // The file now matches what the editor is showing. This is our own save coming
  // back around; adopt the new version token but leave the buffer alone.
  if (input.incomingContent === input.draft) {
    return 'echo';
  }
  // No local edits to lose, so take the new content. This deliberately does not
  // care whether the editor is open: an editor showing exactly savedContent has
  // nothing to conflict with, and warning about a conflict there would be noise.
  if (input.draft === input.savedContent) {
    return 'refresh';
  }
  return 'conflict';
}
