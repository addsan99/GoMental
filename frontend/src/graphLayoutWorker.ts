import Graph from 'graphology';
import forceAtlas2 from 'graphology-layout-forceatlas2';

type LayoutNode = {
  id: string;
  x: number;
  y: number;
  size?: number;
};

type LayoutEdge = {
  id: string;
  source: string;
  target: string;
  weight?: number;
};

type LayoutRequest = {
  type: 'layout';
  nodes: LayoutNode[];
  edges: LayoutEdge[];
  iterations: number;
};

type Coordinates = Record<string, {x: number; y: number}>;

self.onmessage = (event: MessageEvent<LayoutRequest>) => {
  const message = event.data;
  if (message.type !== 'layout') {
    return;
  }

  const graph = new Graph({multi: true, type: 'undirected'});
  for (const node of message.nodes) {
    graph.addNode(node.id, {
      // Every node keeps its seed position and settles freely — no node is
      // anchored, so ForceAtlas2 finds each cluster's natural centroid.
      x: finite(node.x),
      y: finite(node.y),
      size: node.size || 1,
    });
  }
  for (const edge of message.edges) {
    if (!graph.hasNode(edge.source) || !graph.hasNode(edge.target)) {
      continue;
    }
    const key = graph.hasEdge(edge.id) ? `${edge.id}-${graph.size}` : edge.id;
    graph.addUndirectedEdgeWithKey(key, edge.source, edge.target, {weight: edge.weight || 1});
  }

  const iterations = Math.max(1, Math.min(message.iterations || 80, 800));
  const settings = forceAtlas2.inferSettings(graph);
  forceAtlas2.assign(graph, {
    iterations,
    settings: {
      ...settings,
      // LinLog energy model: attraction is logarithmic, so within-cluster nodes
      // pull tight while between-cluster gaps open up — this is what separates
      // the communities (Gephi/Obsidian look).
      linLogMode: true,
      // Dissuade hubs: a hub's pull is shared across its neighbours, so degree-1
      // leaves fan out into petals instead of piling onto the hub. Gives the
      // round, dandelion-style outline.
      outboundAttractionDistribution: true,
      adjustSizes: true,
      barnesHutOptimize: graph.order > 80,
      gravity: 1.0,
      scalingRatio: 3,
      // Honour per-edge weights (hard links strong, soft/metadata weak, hidden
      // links zero) so only visible structure shapes the layout.
      edgeWeightInfluence: 1,
      slowDown: 5,
    },
  });

  const coordinates: Coordinates = {};
  graph.forEachNode((id, attributes) => {
    coordinates[id] = {x: finite(attributes.x), y: finite(attributes.y)};
  });
  self.postMessage({type: 'layout', coordinates});
};

function finite(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

export {};
