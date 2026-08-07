// Maps the client-side GraphViewState onto the backend GraphQueryDTO.
import {application} from '../../../../wailsjs/go/models'
import type {GraphViewState} from './types'

export function toGraphQueryDTO(state: GraphViewState): application.GraphQueryDTO {
  // pathPrefix carries a single folder only. Multi-folder selection is treated
  // as client-side emphasis, not a server-side narrowing, so we leave the
  // prefix empty when more than one folder is selected.
  const folders = state.facets.folders
  const pathPrefix = folders.length === 1 ? folders[0] : ''

  return {
    seed: state.seed ?? '',
    metadataSeed: state.metadataSeed ?? '',
    depth: state.depth,
    types: state.metadataSeed ? [] : state.facets.types,
    tags: state.metadataSeed ? [] : state.facets.tags,
    pathPrefix: state.metadataSeed ? '' : pathPrefix,
    favoritesOnly: state.metadataSeed ? false : state.facets.favorites,
    includeSoftLinks: state.includeSoftLinks,
    includeMetadataLinks: state.includeMetadataLinks,
    includeUnresolved: state.includeUnresolved,
  } as application.GraphQueryDTO
}

export function defaultGraphViewState(): GraphViewState {
  return {
    seed: undefined,
    metadataSeed: undefined,
    depth: 2,
    facets: {types: [], tags: [], folders: [], favorites: false},
    includeSoftLinks: true,
    includeMetadataLinks: true,
    includeUnresolved: false,
    hideNonMatches: false,
    layout: 'force',
    groupBy: undefined,
  }
}
