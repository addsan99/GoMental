// Visible-count reporting shared by the graph renderer.
import type {GraphData, LinkType} from './model';
import {HUB_NODE_KINDS} from './palette';

// Count nodes and the links that are actually visible under the current
// link-type toggles (hub nodes only count when metadata links are on).
export function reportStats(data: GraphData, linkTypes: Record<LinkType, boolean>, onStats: (s: {notes: number; links: number}) => void) {
  let notes = 0;
  for (const node of data.nodes) {
    if (HUB_NODE_KINDS.has(node.kind) && !linkTypes.metadata) {
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
