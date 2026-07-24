// Small shared helpers used across the shell and its child components. Kept in
// one place so the same logic isn't re-implemented per file.
import type {application} from '../wailsjs/go/models';

// Last path segment (handles both / and \ separators), falling back to the
// original string when there is no separator.
export function basename(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() || path;
}

// Display label for a note: its title, else the last segment of its id.
export function noteTitle(note: application.NoteSummaryDTO): string {
  return note.title || note.id.split('/').pop() || note.id;
}

// Best-effort human-readable message for an unknown thrown value.
export function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === 'string') {
    return err;
  }
  try {
    return JSON.stringify(err);
  } catch {
    return 'Unexpected error';
  }
}
