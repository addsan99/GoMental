// Shared facet-matching helpers. Used by both the graph (dim/hide nodes) and the
// App note-list filter, so the two never drift apart.
import type {application} from '../../../../wailsjs/go/models'
import type {FacetFilter} from './types'

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
  return true
}

// True when any facet axis has a selection (so callers can short-circuit).
export function anyFacetActive(facets: FacetFilter): boolean {
  return facets.types.length > 0 || facets.tags.length > 0 || facets.folders.length > 0
}
