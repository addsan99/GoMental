// DTO → graph-model transformation shared by the graph renderer. These are pure
// functions over the transport DTOs; they build the {nodes, links} the force
// graph consumes, compute hop distances from the selected node, and apply a
// persisted flat layout. Kept renderer-agnostic so the 2D (flat) and 3D
// instances share exactly the same model.
import type {application} from '../../../wailsjs/go/models';

export type LinkType = 'hard' | 'soft' | 'metadata';

export type GraphNode = {
  id: string;
  label: string;
  kind: string;
  noteId?: string;
  val: number;
  degree: number;
  selected: boolean;
  // Hop distance (undirected) from the selected/centre node: 0 = selected,
  // 1 = its neighbours, and so on. undefined when nothing is selected (FullGraph)
  // or the node is unreachable. Drives the depth-shaded colouring.
  hops?: number;
  // Simulation position + optional pins (fx/fy/fz), assigned/read by the force
  // graph at runtime. Used by flat mode to seed and persist the {x,y} layout.
  x?: number;
  y?: number;
  z?: number;
  fx?: number;
  fy?: number;
  fz?: number;
};

export type GraphLink = {
  source: string;
  target: string;
  kind: string;
  linkType: LinkType;
};

export type GraphData = {nodes: GraphNode[]; links: GraphLink[]};

export function edgeLinkType(kind: string): LinkType {
  if (kind === 'inferred_related_to') {
    return 'soft';
  }
  if (kind === 'tagged_with' || kind === 'shared_type' || kind === 'shared_heading') {
    return 'metadata';
  }
  return 'hard';
}

export function buildData(dto: application.GraphDTO, selectedGraphID: string, flat: boolean): GraphData {
  const degree: Record<string, number> = {};
  for (const edge of dto.edges) {
    degree[edge.source] = (degree[edge.source] || 0) + 1;
    degree[edge.target] = (degree[edge.target] || 0) + 1;
  }

  const nodes: GraphNode[] = dto.nodes.map((node) => {
    const noteID = node.noteId || (node.kind === 'note' ? node.id : undefined);
    const selected = Boolean(selectedGraphID && (node.id === selectedGraphID || noteID === selectedGraphID));
    const deg = degree[node.id] || 0;
    // Logarithmic bump so well-connected nodes read as bigger (caps out so a
    // single super-hub doesn't dominate). Widened vs. the original so the size
    // difference between leaf and hub notes is actually visible. val feeds sphere
    // volume in ForceGraph3D (radius ∝ cbrt(val)).
    const hubBump = node.kind === 'unresolved' ? 0 : Math.min(14, Math.log2(deg + 1) * 2.6);
    const val = selected ? 18 : (node.kind === 'unresolved' ? 3 : 5) + hubBump;
    const built: GraphNode = {
      id: node.id,
      label: node.label || noteID || node.id,
      kind: node.kind,
      noteId: noteID,
      val,
      degree: deg,
      selected,
    };
    if (flat) {
      // Pin every node to the z=0 plane so the layout stays coplanar even if the
      // 3D simulation would otherwise nudge nodes off it.
      built.z = 0;
      built.fz = 0;
    }
    return built;
  });

  const ids = new Set(nodes.map((n) => n.id));
  const links: GraphLink[] = [];
  for (const edge of dto.edges) {
    if (!ids.has(edge.source) || !ids.has(edge.target)) {
      continue;
    }
    links.push({
      source: edge.source,
      target: edge.target,
      kind: edge.kind,
      linkType: edgeLinkType(edge.kind),
    });
  }

  assignHops(nodes, links);
  return {nodes, links};
}

// Label every node with its undirected hop distance from the selected node via a
// breadth-first sweep over the built edges (0 = selected, 1 = neighbours, …).
// Nodes with nothing selected or that are unreachable keep hops === undefined.
export function assignHops(nodes: GraphNode[], links: GraphLink[]) {
  const source = nodes.find((n) => n.selected);
  if (!source) {
    return;
  }
  const adjacency: Record<string, string[]> = {};
  for (const link of links) {
    (adjacency[link.source] ||= []).push(link.target);
    (adjacency[link.target] ||= []).push(link.source);
  }
  const distance: Record<string, number> = {[source.id]: 0};
  const queue: string[] = [source.id];
  for (let head = 0; head < queue.length; head += 1) {
    const current = queue[head];
    const next = distance[current] + 1;
    for (const neighbour of adjacency[current] || []) {
      if (distance[neighbour] === undefined) {
        distance[neighbour] = next;
        queue.push(neighbour);
      }
    }
  }
  for (const node of nodes) {
    node.hops = distance[node.id];
  }
}

// Seed freshly built flat nodes with persisted {x,y} coordinates and pin them
// (fx/fy/fz) so the saved layout is honoured exactly. Nodes without a saved
// position stay unpinned and settle via the force sim. Returns true if at least
// one node had a finite saved position (i.e. a layout actually carried over).
export function applySavedLayout(nodes: GraphNode[], coordinates: Record<string, application.LayoutCoordinatesDTO>): boolean {
  let applied = false;
  for (const node of nodes) {
    const c = coordinates[node.id];
    if (c && Number.isFinite(c.x) && Number.isFinite(c.y)) {
      node.x = node.fx = c.x;
      node.y = node.fy = c.y;
      node.z = node.fz = 0;
      applied = true;
    }
  }
  return applied;
}
