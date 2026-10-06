// Column alignment for GitHub-flavoured markdown tables.
//
// The delimiter row under the header carries the alignment: a leading colon
// means left, a trailing colon means right, both mean centre, and a bare run of
// dashes means "unspecified". Unspecified renders left, which is what prose
// tables want — the renderer previously right-aligned every column except the
// first on the assumption that column 0 was a label and the rest were numbers,
// which mangled ordinary text tables.

export type ColumnAlign = 'left' | 'center' | 'right';

/**
 * Parse the alignment row of a markdown table (the `|---|:--:|---:|` line).
 * Columns with no explicit alignment default to left.
 */
export function parseTableAlignment(row: string): ColumnAlign[] {
  return row
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map((cell) => {
      const spec = cell.trim();
      const left = spec.startsWith(':');
      const right = spec.endsWith(':');
      if (left && right) {
        return 'center';
      }
      if (right) {
        return 'right';
      }
      return 'left';
    });
}

/**
 * Alignment for one column, tolerating a delimiter row with fewer cells than
 * the header (malformed tables shouldn't throw away the remaining columns).
 */
export function alignOf(align: ColumnAlign[], index: number): ColumnAlign {
  return align[index] ?? 'left';
}
