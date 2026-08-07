// Graph filtering / lens state types.
//
// NOTE: `LayoutKind` is intentionally duplicated with the layout agent's
// `layout/types.ts`. The main session reconciles the two during integration.
export type LayoutKind = 'force' | 'radial' | 'zoned'
export type GroupBy = 'type' | 'folder' | 'tag'

export interface FacetFilter {
  types: string[]
  tags: string[]
  folders: string[]
  favorites: boolean
}

export interface GraphViewState {
  seed?: string // focus note id (undefined = no focus)
  metadataSeed?: string // focus metadata hub id (e.g. tag:go)
  depth: number // 1..3
  facets: FacetFilter
  includeSoftLinks: boolean
  includeMetadataLinks: boolean
  includeUnresolved: boolean
  hideNonMatches: boolean // false = dim non-matches (focus+context); true = hard hide
  layout: LayoutKind
  groupBy?: GroupBy // used by zoned layout / hull grouping
}

export interface LensContext {
  selectedId?: string
}

export interface Lens {
  id: string
  label: string
  description: string
  // Returns a partial state to merge onto the current state when the lens is picked.
  apply(ctx: LensContext): Partial<GraphViewState>
}
