// Visible-count reporting shared by the graph renderer.
import type {GraphData, LinkType} from './model';

// Count nodes and the links that are actually visible under the current
// link-type toggles. Only real notes count towards "notes": facet hubs and
// unresolved link targets are rendered nodes but not notes, and counting them
// made the tally read several times the size of the workspace.
export function reportStats(data: GraphData, linkTypes: Record<LinkType, boolean>, onStats: (s: {notes: number; links: number}) => void) {
  let notes = 0;
  for (const node of data.nodes) {
    if (node.kind !== 'note') {
      continue;
    }
    notes += 1;
  }
  let links = 0;
  for (const link of data.links) {
    if (linkTypes[link.linkType]) {
      links += 1;
    }
  }
  onStats({notes, links});
}
