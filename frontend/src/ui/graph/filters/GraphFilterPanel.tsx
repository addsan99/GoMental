// Presentational graph layout/lens panel. The facet badge lists (Types / Tags /
// Folders) have moved to the shared FacetFilters component in the right rail;
// this panel now owns only the graph-specific controls: layout, group-by, and
// the hide-non-matches toggle.
import type {CSSProperties} from 'react'
import type {GraphViewState, GroupBy, LayoutKind} from './types'

// Static styles hoisted to module scope so they aren't reallocated per render.
const LAYOUT_PICKER_ROW: CSSProperties = {display: 'flex', gap: '4px'}
const LAYOUT_BTN_BASE: CSSProperties = {padding: '4px 10px', borderRadius: '6px'}
const LAYOUT_BTN_DISABLED: CSSProperties = {...LAYOUT_BTN_BASE, opacity: 0.35, cursor: 'not-allowed'}
const LAYOUT_BTN_ACTIVE: CSSProperties = {...LAYOUT_BTN_BASE, opacity: 1, cursor: 'pointer'}
const LAYOUT_BTN_INACTIVE: CSSProperties = {...LAYOUT_BTN_BASE, opacity: 0.6, cursor: 'pointer'}

interface GraphFilterPanelProps {
  state: GraphViewState
  matchCount: number
  totalCount: number
  onChange: (next: GraphViewState) => void
  // Zoned draws pinned hulls in the flat plane; it's disabled for the orbit (3D)
  // instance, where the hulls don't render. The button is shown but inert there.
  allowZoned: boolean
}

const LAYOUTS: Array<{value: LayoutKind; label: string}> = [
  {value: 'force', label: 'Force'},
  {value: 'radial', label: 'Radial'},
  {value: 'zoned', label: 'Zoned'},
]

const GROUP_BYS: Array<{value: GroupBy; label: string}> = [
  {value: 'type', label: 'Type'},
  {value: 'folder', label: 'Folder'},
  {value: 'tag', label: 'Tag'},
]

export function GraphFilterPanel(props: GraphFilterPanelProps) {
  const {state, matchCount, totalCount, onChange, allowZoned} = props

  return (
    <div className="gm-graph-filter-panel">
      <div className="gm-graph-opt">
        <span className="gm-graph-opt-label">Layout</span>
        <div className="gm-graph-layout-picker" style={LAYOUT_PICKER_ROW}>
          {LAYOUTS.map((layout) => {
            const active = state.layout === layout.value
            const disabled = layout.value === 'zoned' && !allowZoned
            return (
              <button
                key={layout.value}
                type="button"
                className={`gm-graph-layout-btn${active ? ' is-active' : ''}`}
                aria-pressed={active}
                disabled={disabled}
                title={disabled ? 'Zoned layout is available in the 2D view.' : undefined}
                onClick={() => onChange({...state, layout: layout.value})}
                style={disabled ? LAYOUT_BTN_DISABLED : active ? LAYOUT_BTN_ACTIVE : LAYOUT_BTN_INACTIVE}
              >
                {layout.label}
              </button>
            )
          })}
        </div>
        {state.layout === 'zoned' && (
          <>
            <label className="gm-graph-opt-label sub" htmlFor="gm-graph-groupby">
              Group by
            </label>
            <select
              id="gm-graph-groupby"
              value={state.groupBy ?? 'folder'}
              onChange={(event) => onChange({...state, groupBy: event.target.value as GroupBy})}
            >
              {GROUP_BYS.map((group) => (
                <option key={group.value} value={group.value}>
                  {group.label}
                </option>
              ))}
            </select>
          </>
        )}
      </div>

      <div className="gm-graph-opt">
        <label className="gm-graph-check">
          <input
            type="checkbox"
            checked={state.hideNonMatches}
            onChange={() => onChange({...state, hideNonMatches: !state.hideNonMatches})}
          />
          Hide non-matches
        </label>
        <p className="gm-graph-opt-hint">
          {matchCount} of {totalCount} notes match
        </p>
      </div>
    </div>
  )
}
