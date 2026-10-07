// Shared facet-matching helpers. Used by both the graph (dim/hide nodes) and the
// App note-list filter, so the two never drift apart.
import type {application} from '../../../../wailsjs/go/models'
import type {FacetFilter} from './types'
import {isRecentNote} from '../../../noteSort'

// Top-level folder segment of a note path (the grouping/facet key for folders).
export function folderOf(path: string): string {
  if (!path) {
    return 'root'
  }
  const norm = path.replace(/\\/g, '/')
  const idx = norm.indexOf('/')
  return idx === -1 ? 'root' : norm.slice(0, idx)
}

// Whether a note satisfies the facet filter (AND across axes, OR within an axis).
export function facetMatchesNote(note: application.NoteSummaryDTO | undefined, facets: FacetFilter): boolean {
  if (!note) {
    return false
  }
  if (facets.types.length && !facets.types.includes(note.type)) {
    return false
  }
  if (facets.tags.length && !(note.tags || []).some((tag) => facets.tags.includes(tag))) {
    return false
  }
  if (facets.folders.length && !facets.folders.some((folder) => folderOf(note.path) === folder)) {
    return false
  }
  if (facets.favorites && !note.favorite) {
    return false
  }
  // `now` is read per call rather than captured, so a long-running window keeps
  // agreeing with the clock instead of freezing at mount time.
  if (facets.recent && !isRecentNote(note.modifiedAt, Date.now())) {
    return false
  }
  return true
}

// True when any facet axis has a selection (so callers can short-circuit).
export function anyFacetActive(facets: FacetFilter): boolean {
  return facets.types.length > 0
    || facets.tags.length > 0
    || facets.folders.length > 0
    || facets.favorites
    || Boolean(facets.recent)
}

// Which display filters would hide a note the app just created or imported.
//
// Filters and the search box are display-only — they never change which note is
// selected — but a brand new note matches neither an active tag facet nor the
// current query, so the sidebar would list everything except the note the user
// just asked for. A search query always hides it: the index is asynchronous, so
// even a note that would match is missing from the current hits.
export function filtersHidingNote(
  searchText: string,
  facets: FacetFilter,
  note: application.NoteSummaryDTO | undefined,
): {search: boolean; facets: boolean} {
  return {
    search: searchText.trim().length > 0,
    facets: anyFacetActive(facets) && !facetMatchesNote(note, facets),
  }
}
