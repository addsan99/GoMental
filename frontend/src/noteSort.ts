// Time-based ordering and bucketing for the note list.
//
// Kept free of React so the date arithmetic — which is the part that actually
// goes wrong — can be tested directly against a fixed "now".

import type {application} from '../wailsjs/go/models';

export type NoteSort = 'name' | 'recent';

export const NOTE_SORTS: {value: NoteSort; label: string}[] = [
  {value: 'name', label: 'Name'},
  {value: 'recent', label: 'Recently modified'},
];

/** Bucket labels, in the order they should appear. */
export const RECENCY_BUCKETS = ['Today', 'Yesterday', 'This week', 'This month', 'Older', 'Undated'] as const;

export type RecencyBucket = (typeof RECENCY_BUCKETS)[number];

/** A note is "recent" within this many hours. Rolling, not calendar-based. */
export const RECENT_WINDOW_HOURS = 24;

export function isNoteSort(value: unknown): value is NoteSort {
  return value === 'name' || value === 'recent';
}

/**
 * Milliseconds since the epoch, or NaN when the stamp is missing or unparseable.
 * Notes carry whatever the filesystem reported, so a blank or malformed value is
 * a normal case rather than a bug.
 */
export function noteTime(iso: string | undefined): number {
  if (!iso) {
    return Number.NaN;
  }
  return new Date(iso).getTime();
}

/** Whether a note was touched inside the rolling recent window. */
export function isRecentNote(iso: string | undefined, now: number): boolean {
  const time = noteTime(iso);
  if (Number.isNaN(time)) {
    return false;
  }
  // Clock skew or a filesystem stamped slightly ahead of us still counts: a note
  // in the future is certainly not old.
  return time >= now - RECENT_WINDOW_HOURS * 3600_000;
}

function startOfDay(at: number): number {
  const date = new Date(at);
  date.setHours(0, 0, 0, 0);
  return date.getTime();
}

/**
 * Which bucket a timestamp belongs to, relative to `now`.
 *
 * Boundaries are calendar-based rather than rolling, because "yesterday" to a
 * person means the previous calendar day, not 24-48 hours ago. The week starts
 * on Monday.
 */
export function recencyBucket(iso: string | undefined, now: number): RecencyBucket {
  const time = noteTime(iso);
  if (Number.isNaN(time)) {
    return 'Undated';
  }
  const today = startOfDay(now);
  // A note stamped in the future reads as today; the alternative is burying it
  // under "Older", which is the opposite of the truth.
  if (time >= today) {
    return 'Today';
  }
  if (time >= today - 86_400_000) {
    return 'Yesterday';
  }
  // Monday-based week: getDay() is 0 for Sunday, which belongs to the week that
  // started six days earlier.
  const weekday = new Date(today).getDay();
  const startOfWeek = today - ((weekday + 6) % 7) * 86_400_000;
  if (time >= startOfWeek) {
    return 'This week';
  }
  const monthStart = new Date(now);
  monthStart.setDate(1);
  monthStart.setHours(0, 0, 0, 0);
  if (time >= monthStart.getTime()) {
    return 'This month';
  }
  return 'Older';
}

/**
 * Notes newest first. Undated notes sort last, then by id so the order is
 * stable rather than dependent on how the workspace happened to be scanned.
 */
export function sortByRecency(notes: application.NoteSummaryDTO[]): application.NoteSummaryDTO[] {
  return [...notes].sort((a, b) => {
    const left = noteTime(a.modifiedAt);
    const right = noteTime(b.modifiedAt);
    const leftMissing = Number.isNaN(left);
    const rightMissing = Number.isNaN(right);
    if (leftMissing || rightMissing) {
      if (leftMissing && rightMissing) {
        return a.id.localeCompare(b.id);
      }
      return leftMissing ? 1 : -1;
    }
    if (left !== right) {
      return right - left;
    }
    return a.id.localeCompare(b.id);
  });
}

export type RecencyGroup = {
  name: string;
  notes: application.NoteSummaryDTO[];
};

/**
 * Notes grouped into time buckets, newest bucket first, newest note first
 * within each. Empty buckets are omitted so the sidebar never shows a heading
 * with nothing under it.
 */
export function groupByRecency(notes: application.NoteSummaryDTO[], now: number): RecencyGroup[] {
  const buckets = new Map<RecencyBucket, application.NoteSummaryDTO[]>();
  for (const note of sortByRecency(notes)) {
    const bucket = recencyBucket(note.modifiedAt, now);
    const items = buckets.get(bucket);
    if (items) {
      items.push(note);
    } else {
      buckets.set(bucket, [note]);
    }
  }
  // Iterate the canonical order rather than the Map's insertion order: a
  // workspace whose oldest note is scanned first would otherwise invert it.
  return RECENCY_BUCKETS
    .filter((bucket) => buckets.has(bucket))
    .map((bucket) => ({name: bucket, notes: buckets.get(bucket) as application.NoteSummaryDTO[]}));
}
