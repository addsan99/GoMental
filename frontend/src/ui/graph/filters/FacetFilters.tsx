// Presentational facet badge lists (Types / Tags / Folders). Owned by App and
// rendered in the right rail; the same selection drives both the note-list tree
// and the graph. No data fetching — the parent derives `available` facets
// (ranked by note count) and passes state; this component emits changes.
import {useState} from 'react'
import type {CSSProperties} from 'react'
import type {FacetFilter} from './types'

type FacetAxis = keyof FacetFilter

// A selectable facet value plus the number of notes carrying it (for ranking and
// the count badge).
export interface FacetOption {
  value: string
  count: number
}

// Collapse a facet group to this many chips before offering "+N more"; show a
// search box once a group has more than SEARCH_MIN_OPTIONS values.
const MAX_VISIBLE_CHIPS = 12
const SEARCH_MIN_OPTIONS = 16

// Chip styles are stable, so precompute the variants once at module scope rather
// than allocating a fresh object per chip on every render.
const CHIP_BASE: CSSProperties = {
  padding: '2px 8px',
  borderRadius: '999px',
  border: '1px solid currentColor',
  cursor: 'pointer',
}
const CHIP_ACTIVE: CSSProperties = {...CHIP_BASE, opacity: 1}
const CHIP_INACTIVE: CSSProperties = {...CHIP_BASE, opacity: 0.6}
const CHIP_MORE: CSSProperties = {...CHIP_INACTIVE, borderStyle: 'dashed'}
const CHIPS_ROW: CSSProperties = {display: 'flex', flexWrap: 'wrap', gap: '4px'}
const SEARCH_INPUT: CSSProperties = {margin: '2px 0 4px', width: '100%', boxSizing: 'border-box'}

interface FacetFiltersProps {
  available: {types: FacetOption[]; tags: FacetOption[]; folders: FacetOption[]}
  facets: FacetFilter
  matchCount: number
  totalCount: number
  onChange: (next: FacetFilter) => void
}

function toggleMembership(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value]
}

export function FacetFilters(props: FacetFiltersProps) {
  const {available, facets, matchCount, totalCount, onChange} = props
  // Per-axis expand + search state so each facet group manages its own long tail.
  const [expanded, setExpanded] = useState<Record<FacetAxis, boolean>>({types: false, tags: false, folders: false})
  const [queries, setQueries] = useState<Record<FacetAxis, string>>({types: '', tags: '', folders: ''})

  const toggleFacet = (axis: FacetAxis, value: string) => {
    onChange({...facets, [axis]: toggleMembership(facets[axis], value)})
  }

  const renderFacetGroup = (axis: FacetAxis, label: string, options: FacetOption[]) => {
    if (options.length === 0) return null
    const selected = facets[axis]
    const isExpanded = expanded[axis]
    const query = queries[axis].trim().toLowerCase()

    // Selected chips always show. The rest is filtered by the search box and,
    // when collapsed, capped so the group doesn't become an endless wall.
    const selectedOpts = options.filter((option) => selected.includes(option.value))
    const pool = options.filter(
      (option) => !selected.includes(option.value) && (query === '' || option.value.toLowerCase().includes(query)),
    )
    const room = Math.max(0, MAX_VISIBLE_CHIPS - selectedOpts.length)
    const shownPool = isExpanded ? pool : pool.slice(0, room)
    const hiddenCount = pool.length - shownPool.length
    const visible = [...selectedOpts, ...shownPool]

    return (
      <div className="gm-graph-opt">
        <span className="gm-graph-opt-label">{label}</span>
        {options.length > SEARCH_MIN_OPTIONS && (
          <input
            className="gm-graph-facet-search"
            type="search"
            value={queries[axis]}
            placeholder={`Filter ${label.toLowerCase()}…`}
            onChange={(event) => setQueries((current) => ({...current, [axis]: event.target.value}))}
            style={SEARCH_INPUT}
          />
        )}
        <div className="gm-graph-chips" style={CHIPS_ROW}>
          {visible.map((option) => {
            const active = selected.includes(option.value)
            return (
              <button
                key={option.value}
                type="button"
                className={`gm-graph-chip${active ? ' is-active' : ''}`}
                aria-pressed={active}
                title={`${option.count} note${option.count === 1 ? '' : 's'}`}
                onClick={() => toggleFacet(axis, option.value)}
                style={active ? CHIP_ACTIVE : CHIP_INACTIVE}
              >
                {option.value}
                <span style={{opacity: 0.6, marginLeft: 4}}>{option.count}</span>
              </button>
            )
          })}
          {!isExpanded && hiddenCount > 0 && (
            <button
              type="button"
              className="gm-graph-chip gm-graph-chip--more"
              onClick={() => setExpanded((current) => ({...current, [axis]: true}))}
              style={CHIP_MORE}
            >
              +{hiddenCount} more
            </button>
          )}
          {isExpanded && pool.length > room && (
            <button
              type="button"
              className="gm-graph-chip gm-graph-chip--less"
              onClick={() => setExpanded((current) => ({...current, [axis]: false}))}
              style={CHIP_MORE}
            >
              Show less
            </button>
          )}
        </div>
        {query !== '' && shownPool.length === 0 && (
          <p className="gm-graph-opt-hint">No {label.toLowerCase()} match “{queries[axis]}”.</p>
        )}
      </div>
    )
  }

  const hasAny = available.types.length > 0 || available.tags.length > 0 || available.folders.length > 0
  if (!hasAny) {
    return <div className="gm-rail-empty">No facets in this workspace.</div>
  }

  return (
    <div className="gm-facet-filters">
      {renderFacetGroup('types', 'Types', available.types)}
      {renderFacetGroup('tags', 'Tags', available.tags)}
      {renderFacetGroup('folders', 'Folders', available.folders)}
      <p className="gm-graph-opt-hint">
        {matchCount} of {totalCount} notes match
      </p>
    </div>
  )
}
