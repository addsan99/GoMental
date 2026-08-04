import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import Graph from 'graphology';
import Sigma from 'sigma';
import type {NodeHoverDrawingFunction, NodeLabelDrawingFunction} from 'sigma/rendering';
import {FullGraph, LoadGraphLayout, Neighborhood, SaveGraphLayout} from './transport';
import {application} from '../wailsjs/go/models';

type GraphMode = 'local' | 'global';
type GraphStatus = 'idle' | 'loading' | 'layout' | 'ready' | 'error';

type GraphPanelProps = {
  workspaceOpen: boolean;
  selectedID: string;
  notes: application.NoteSummaryDTO[];
  refreshKey?: number;
  onSelectNote: (id: string) => void;
  onOpenNote: (id: string) => void;
  onError: (message: string) => void;
  theme?: 'light' | 'dark';
};

type Coordinates = Record<string, {x: number; y: number}>;

type GraphNodeAttributes = {
  x: number;
  y: number;
  size: number;
  label: string;
  color: string;
  kind: string;
  noteId?: string;
  hidden?: boolean;
  highlighted?: boolean;
  forceLabel?: boolean;
};

type GraphEdgeAttributes = {
  size: number;
  color: string;
  kind: string;
  hidden?: boolean;
  weight?: number;
};

type LayoutWorkerResponse = {
  type: 'layout';
  coordinates: Coordinates;
};

const localDepthOptions = [1, 2, 3];

function GraphPanel({workspaceOpen, selectedID, notes, refreshKey = 0, onSelectNote, onOpenNote, onError, theme = 'light'}: GraphPanelProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const sigmaRef = useRef<Sigma<GraphNodeAttributes, GraphEdgeAttributes> | null>(null);
  const graphRef = useRef<Graph<GraphNodeAttributes, GraphEdgeAttributes> | null>(null);
  const workerRef = useRef<Worker | null>(null);
  const layoutRef = useRef<Coordinates>({});
  const requestRef = useRef(0);
  const [mode, setMode] = useState<GraphMode>('local');
  const [depth, setDepth] = useState(1);
  const [includeSoftLinks, setIncludeSoftLinks] = useState(true);
  const [includeUnresolved, setIncludeUnresolved] = useState(true);
  const [pathPrefix, setPathPrefix] = useState('');
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [nodeSearch, setNodeSearch] = useState('');
  const [status, setStatus] = useState<GraphStatus>('idle');
  const [summary, setSummary] = useState('');
  const [hoveredNode, setHoveredNode] = useState<string>('');
  const clickTimerRef = useRef<number | null>(null);
  const clickNodeRef = useRef<string>('');

  const allTags = useMemo(() => Array.from(new Set(notes.flatMap((note) => note.tags || []))).sort((a, b) => a.localeCompare(b)).slice(0, 12), [notes]);

  const refreshGraph = useCallback(async () => {
    if (!workspaceOpen) {
      setStatus('idle');
      setSummary('Open a workspace to inspect the graph.');
      layoutRef.current = {};
      return;
    }
    if (mode === 'local' && !selectedID) {
      setStatus('idle');
      setSummary('Select a note to inspect its local graph.');
      return;
    }
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    setStatus('loading');
    try {
      const [dto, savedLayout] = await Promise.all([
        mode === 'local'
          ? Neighborhood(selectedID, depth)
          : FullGraph({
            pathPrefix: normalizeGraphPath(pathPrefix),
            tags: selectedTags,
            favoritesOnly: false,
            includeUnresolved,
            includeSoftLinks,
            includeMetadataLinks: true,
            depth: 0,
          }),
        LoadGraphLayout(),
      ]);
      if (requestRef.current !== requestID) {
        return;
      }
      layoutRef.current = normalizeCoordinates(savedLayout.coordinates || {});
      const graph = graphFromDTO(dto, selectedID, nodeSearch, layoutRef.current, theme);
      renderGraph(graph, containerRef.current, sigmaRef, graphRef, clickTimerRef, clickNodeRef, onSelectNote, onOpenNote, setHoveredNode, theme);
      setSummary(`${dto.nodes.length} nodes, ${dto.edges.length} edges`);
      setStatus('layout');
      runLayoutWorker(graph, workerRef, async (coordinates) => {
        if (requestRef.current !== requestID) {
          return;
        }
        layoutRef.current = {...layoutRef.current, ...coordinates};
        applyCoordinates(graphRef.current, coordinates);
        sigmaRef.current?.refresh();
        setStatus('ready');
        try {
          await SaveGraphLayout(application.LayoutSnapshotDTO.createFrom({coordinates: layoutRef.current, updatedAt: new Date().toISOString()}));
        } catch (err) {
          onError(errorMessage(err));
        }
      });
    } catch (err) {
      if (requestRef.current !== requestID) {
        return;
      }
      const message = errorMessage(err);
      setStatus('error');
      setSummary(message);
      onError(message);
    }
  }, [depth, includeSoftLinks, includeUnresolved, mode, nodeSearch, onError, onOpenNote, onSelectNote, pathPrefix, refreshKey, selectedID, selectedTags, theme, workspaceOpen]);

  useEffect(() => {
    void refreshGraph();
  }, [refreshGraph]);

  useEffect(() => () => {
    workerRef.current?.terminate();
    workerRef.current = null;
    if (clickTimerRef.current !== null) {
      window.clearTimeout(clickTimerRef.current);
      clickTimerRef.current = null;
    }
    sigmaRef.current?.kill();
    sigmaRef.current = null;
    graphRef.current = null;
  }, []);

  useEffect(() => {
    const graph = graphRef.current;
    const sigma = sigmaRef.current;
    if (!graph || !sigma) {
      return;
    }
    const neighbors = hoveredNode ? new Set(graph.neighbors(hoveredNode).concat(hoveredNode)) : null;
    graph.forEachNode((node) => {
      graph.mergeNodeAttributes(node, {
        hidden: false,
        highlighted: Boolean(neighbors?.has(node)),
        forceLabel: node === hoveredNode || Boolean(graph.getNodeAttribute(node, 'noteId') && graph.getNodeAttribute(node, 'noteId') === selectedID),
      });
    });
    graph.forEachEdge((edge, _attributes, source, target) => {
      graph.mergeEdgeAttributes(edge, {hidden: Boolean(neighbors && (!neighbors.has(source) || !neighbors.has(target)))});
    });
    sigma.refresh();
  }, [hoveredNode]);

  const toggleTag = useCallback((tag: string) => {
    setSelectedTags((current) => current.includes(tag) ? current.filter((item) => item !== tag) : [...current, tag]);
  }, []);

  const resetCamera = useCallback(() => {
    sigmaRef.current?.getCamera().animatedReset({duration: 220});
  }, []);

  return (
    <section className="graph-panel" aria-label="Knowledge graph">
      <div className="panel-title">
        <span>Graph</span>
        <span className="count">{status === 'loading' ? 'Loading' : status === 'layout' ? 'Layout' : summary}</span>
      </div>
      <div className="graph-tabs" role="tablist" aria-label="Graph scope">
        <button className={mode === 'local' ? 'active' : ''} type="button" onClick={() => setMode('local')}>Local</button>
        <button className={mode === 'global' ? 'active' : ''} type="button" onClick={() => setMode('global')}>Global</button>
      </div>
      <div className="graph-controls">
        {mode === 'local' && (
          <label>
            <span>Depth</span>
            <select value={depth} onChange={(event) => setDepth(Number(event.target.value))}>
              {localDepthOptions.map((item) => <option value={item} key={item}>{item}</option>)}
            </select>
          </label>
        )}
        {mode === 'global' && (
          <label>
            <span>Path</span>
            <input value={pathPrefix} onChange={(event) => setPathPrefix(event.target.value)} placeholder="folder/" />
          </label>
        )}
        <label>
          <span>Find</span>
          <input value={nodeSearch} onChange={(event) => setNodeSearch(event.target.value)} placeholder="node" />
        </label>
      </div>
      {mode === 'global' && (
        <>
          <div className="graph-toggles">
            <label><input type="checkbox" checked={includeSoftLinks} onChange={(event) => setIncludeSoftLinks(event.target.checked)} /> Soft links</label>
            <label><input type="checkbox" checked={includeUnresolved} onChange={(event) => setIncludeUnresolved(event.target.checked)} /> Unresolved</label>
          </div>
          {allTags.length > 0 && (
            <div className="graph-tags" aria-label="Graph tag filters">
              {allTags.map((tag) => (
                <button className={selectedTags.includes(tag) ? 'active' : ''} type="button" onClick={() => toggleTag(tag)} key={tag}>#{tag}</button>
              ))}
            </div>
          )}
        </>
      )}
      <div className="graph-toolbar">
        <button type="button" onClick={() => void refreshGraph()} disabled={!workspaceOpen || status === 'loading' || status === 'layout'}>Refresh</button>
        <button type="button" onClick={resetCamera} disabled={status !== 'ready'}>Reset</button>
      </div>
      <div className="graph-canvas" ref={containerRef} />
      {status === 'idle' && <p className="graph-empty">{summary}</p>}
      {status === 'error' && <p className="graph-empty error">{summary}</p>}
    </section>
  );
}

function renderGraph(
  graph: Graph<GraphNodeAttributes, GraphEdgeAttributes>,
  container: HTMLDivElement | null,
  sigmaRef: React.MutableRefObject<Sigma<GraphNodeAttributes, GraphEdgeAttributes> | null>,
  graphRef: React.MutableRefObject<Graph<GraphNodeAttributes, GraphEdgeAttributes> | null>,
  clickTimerRef: React.MutableRefObject<number | null>,
  clickNodeRef: React.MutableRefObject<string>,
  onSelectNote: (id: string) => void,
  onOpenNote: (id: string) => void,
  setHoveredNode: (id: string) => void,
  theme: 'light' | 'dark',
) {
  if (!container) {
    return;
  }
  graphRef.current = graph;
  const labelRenderers = graphLabelRenderers(theme);
  const labelColor = theme === 'dark' ? '#edf2f3' : '#22252a';
  if (!sigmaRef.current) {
    const renderer = new Sigma(graph, container, {
      allowInvalidContainer: true,
      defaultEdgeType: 'line',
      defaultNodeColor: '#2d6f7f',
      defaultEdgeColor: '#b8b0a1',
      renderEdgeLabels: false,
      labelRenderedSizeThreshold: 13,
      labelDensity: 0.04,
      labelColor: {color: labelColor},
      labelSize: 11,
      labelFont: 'Segoe UI, Arial, sans-serif',
      labelWeight: 'normal',
      defaultDrawNodeLabel: labelRenderers.label,
      defaultDrawNodeHover: labelRenderers.hover,
      doubleClickZoomingRatio: 1,
      doubleClickZoomingDuration: 0,
      enableEdgeEvents: true,
    });
    renderer.on('clickNode', ({node}) => {
      const noteID = noteIDForGraphNode(graphRef.current, node);
      if (!noteID) {
        return;
      }
      if (clickTimerRef.current !== null) {
        window.clearTimeout(clickTimerRef.current);
      }
      clickNodeRef.current = node;
      clickTimerRef.current = window.setTimeout(() => {
        clickTimerRef.current = null;
        clickNodeRef.current = '';
        onSelectNote(noteID);
      }, 240);
    });
    renderer.on('doubleClickNode', ({node, preventSigmaDefault}) => {
      preventSigmaDefault();
      const noteID = noteIDForGraphNode(graphRef.current, node);
      if (!noteID) {
        return;
      }
      if (clickTimerRef.current !== null) {
        window.clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
      }
      clickNodeRef.current = '';
      onOpenNote(noteID);
    });
    renderer.on('enterNode', ({node}) => setHoveredNode(node));
    renderer.on('leaveNode', () => setHoveredNode(''));
    sigmaRef.current = renderer;
  } else {
    sigmaRef.current.setSetting('labelColor', {color: labelColor});
    sigmaRef.current.setSetting('labelFont', 'Segoe UI, Arial, sans-serif');
    sigmaRef.current.setSetting('labelWeight', 'normal');
    sigmaRef.current.setSetting('defaultDrawNodeLabel', labelRenderers.label);
    sigmaRef.current.setSetting('defaultDrawNodeHover', labelRenderers.hover);
    sigmaRef.current.setGraph(graph);
    sigmaRef.current.refresh();
  }
  sigmaRef.current.getCamera().animatedReset({duration: 180});
}

function noteIDForGraphNode(graph: Graph<GraphNodeAttributes, GraphEdgeAttributes> | null, node: string): string {
  if (!graph?.hasNode(node)) {
    return '';
  }
  return graph.getNodeAttribute(node, 'noteId') || '';
}

function graphFromDTO(dto: application.GraphDTO, selectedID: string, nodeSearch: string, saved: Coordinates, theme: 'light' | 'dark'): Graph<GraphNodeAttributes, GraphEdgeAttributes> {
  const graph = new Graph<GraphNodeAttributes, GraphEdgeAttributes>({multi: true, type: 'directed'});
  const query = nodeSearch.trim().toLocaleLowerCase();
  const visibleNodes = query
    ? new Set(dto.nodes.filter((node) => `${node.label} ${node.id} ${node.noteId || ''}`.toLocaleLowerCase().includes(query)).map((node) => node.id))
    : null;
  const positioned = initialPositions(dto.nodes, saved);

  for (const node of dto.nodes) {
    const position = positioned[node.id] || {x: 0, y: 0};
    const noteID = node.noteId || (node.kind === 'note' ? node.id : undefined);
    const isSelected = Boolean(noteID && noteID === selectedID);
    const matches = !visibleNodes || visibleNodes.has(node.id);
    graph.addNode(node.id, {
      x: position.x,
      y: position.y,
      size: isSelected ? 11 : node.kind === 'unresolved' ? 5 : 7,
      label: node.label || noteID || node.id,
      color: isSelected ? '#bf553f' : node.kind === 'unresolved' ? '#8b8170' : '#2d6f7f',
      kind: node.kind,
      noteId: noteID,
      hidden: !matches,
      forceLabel: isSelected || Boolean(visibleNodes?.has(node.id)),
    });
  }

  for (const edge of dto.edges) {
    if (!graph.hasNode(edge.source) || !graph.hasNode(edge.target)) {
      continue;
    }
    const hidden = Boolean(visibleNodes && (!visibleNodes.has(edge.source) || !visibleNodes.has(edge.target)));
    graph.addDirectedEdgeWithKey(edge.id || `${edge.source}->${edge.target}-${edge.kind}`, edge.source, edge.target, {
      size: edge.kind === 'inferred_related_to' ? 1 : 1.6,
      color: edge.kind === 'inferred_related_to' ? themeAwareEdgeColor('soft', theme) : themeAwareEdgeColor('hard', theme),
      kind: edge.kind,
      hidden,
      weight: edge.kind === 'inferred_related_to' ? 0.55 : 1,
    });
  }

  return graph;
}


function themeAwareEdgeColor(kind: 'hard' | 'soft', theme: 'light' | 'dark'): string {
  if (theme === 'dark') {
    return kind === 'soft' ? '#8f865f' : '#5f858b';
  }
  return kind === 'soft' ? '#c4b88a' : '#8aa9aa';
}

function graphLabelRenderers(theme: 'light' | 'dark'): {label: NodeLabelDrawingFunction<GraphNodeAttributes, GraphEdgeAttributes>; hover: NodeHoverDrawingFunction<GraphNodeAttributes, GraphEdgeAttributes>} {
  const dark = theme === 'dark';
  const labelText = dark ? '#eef6f7' : '#22252a';
  const hoverText = dark ? '#f8fbfb' : '#20272b';
  const hoverBackground = dark ? 'rgba(24, 29, 31, 0.96)' : 'rgba(255, 253, 248, 0.96)';
  const hoverBorder = dark ? 'rgba(125, 216, 222, 0.38)' : 'rgba(45, 111, 127, 0.2)';
  const hoverShadow = dark ? 'rgba(0, 0, 0, 0.5)' : 'rgba(31, 40, 42, 0.16)';

  const label: NodeLabelDrawingFunction<GraphNodeAttributes, GraphEdgeAttributes> = (context, data, settings) => {
    if (!data.label) {
      return;
    }
    const fontSize = typeof settings.labelSize === 'number' ? settings.labelSize : 11;
    const font = typeof settings.labelFont === 'string' ? settings.labelFont : 'Segoe UI, Arial, sans-serif';
    const weight = typeof settings.labelWeight === 'string' ? settings.labelWeight : 'normal';
    context.save();
    context.font = `${weight} ${fontSize}px ${font}`;
    context.textBaseline = 'middle';
    context.fillStyle = labelText;
    if (dark) {
      context.shadowColor = 'rgba(0, 0, 0, 0.72)';
      context.shadowBlur = 3;
      context.shadowOffsetX = 0;
      context.shadowOffsetY = 1;
    }
    context.fillText(data.label, data.x + data.size + 5, data.y);
    context.restore();
  };

  const hover: NodeHoverDrawingFunction<GraphNodeAttributes, GraphEdgeAttributes> = (context, data, settings) => {
    const fontSize = typeof settings.labelSize === 'number' ? settings.labelSize : 11;
    const font = typeof settings.labelFont === 'string' ? settings.labelFont : 'Segoe UI, Arial, sans-serif';
    const weight = typeof settings.labelWeight === 'string' ? settings.labelWeight : 'normal';
    const paddingX = 7;
    const paddingY = 4;
    const labelX = data.x + data.size + 7;
    const labelY = data.y;

    context.save();
    context.beginPath();
    context.arc(data.x, data.y, data.size + 2, 0, Math.PI * 2);
    context.fillStyle = data.color;
    context.fill();
    context.lineWidth = 2;
    context.strokeStyle = dark ? '#d8f5f7' : '#fffdf8';
    context.stroke();

    if (data.label) {
      context.font = `${weight} ${fontSize}px ${font}`;
      context.textBaseline = 'middle';
      const textWidth = context.measureText(data.label).width;
      const boxWidth = textWidth + paddingX * 2;
      const boxHeight = fontSize + paddingY * 2;
      const boxX = labelX - paddingX;
      const boxY = labelY - boxHeight / 2;
      context.shadowColor = hoverShadow;
      context.shadowBlur = dark ? 6 : 4;
      context.shadowOffsetX = 0;
      context.shadowOffsetY = 2;
      drawRoundedRect(context, boxX, boxY, boxWidth, boxHeight, 3);
      context.fillStyle = hoverBackground;
      context.fill();
      context.shadowColor = 'transparent';
      context.lineWidth = 1;
      context.strokeStyle = hoverBorder;
      context.stroke();
      context.fillStyle = hoverText;
      context.fillText(data.label, labelX, labelY);
    }
    context.restore();
  };

  return {label, hover};
}

function drawRoundedRect(context: CanvasRenderingContext2D, x: number, y: number, width: number, height: number, radius: number) {
  const right = x + width;
  const bottom = y + height;
  context.beginPath();
  context.moveTo(x + radius, y);
  context.lineTo(right - radius, y);
  context.quadraticCurveTo(right, y, right, y + radius);
  context.lineTo(right, bottom - radius);
  context.quadraticCurveTo(right, bottom, right - radius, bottom);
  context.lineTo(x + radius, bottom);
  context.quadraticCurveTo(x, bottom, x, bottom - radius);
  context.lineTo(x, y + radius);
  context.quadraticCurveTo(x, y, x + radius, y);
  context.closePath();
}
function runLayoutWorker(
  graph: Graph<GraphNodeAttributes, GraphEdgeAttributes>,
  workerRef: React.MutableRefObject<Worker | null>,
  onLayout: (coordinates: Coordinates) => void | Promise<void>,
) {
  workerRef.current?.terminate();
  const worker = new Worker(new URL('./graphLayoutWorker.ts', import.meta.url), {type: 'module'});
  workerRef.current = worker;
  worker.onmessage = (event: MessageEvent<LayoutWorkerResponse>) => {
    if (event.data.type === 'layout') {
      void onLayout(event.data.coordinates);
    }
    worker.terminate();
    if (workerRef.current === worker) {
      workerRef.current = null;
    }
  };
  worker.onerror = () => {
    worker.terminate();
    if (workerRef.current === worker) {
      workerRef.current = null;
    }
  };
  worker.postMessage({
    type: 'layout',
    nodes: graph.nodes().map((id) => ({
      id,
      x: graph.getNodeAttribute(id, 'x'),
      y: graph.getNodeAttribute(id, 'y'),
      size: graph.getNodeAttribute(id, 'size'),
    })),
    edges: graph.edges().map((id) => ({
      id,
      source: graph.source(id),
      target: graph.target(id),
      weight: graph.getEdgeAttribute(id, 'weight') || 1,
    })),
    iterations: graph.order > 120 ? 90 : 140,
  });
}

function applyCoordinates(graph: Graph<GraphNodeAttributes, GraphEdgeAttributes> | null, coordinates: Coordinates) {
  if (!graph) {
    return;
  }
  for (const [id, point] of Object.entries(coordinates)) {
    if (graph.hasNode(id)) {
      graph.mergeNodeAttributes(id, {x: point.x, y: point.y});
    }
  }
}

function initialPositions(nodes: application.GraphNodeDTO[], saved: Coordinates): Coordinates {
  const positions: Coordinates = {};
  const count = Math.max(nodes.length, 1);
  const radius = Math.max(1, Math.sqrt(count) * 3);
  nodes.forEach((node, index) => {
    const persisted = saved[node.id];
    if (persisted && Number.isFinite(persisted.x) && Number.isFinite(persisted.y)) {
      positions[node.id] = persisted;
      return;
    }
    const angle = (Math.PI * 2 * index) / count;
    const ring = 0.65 + (index % 5) * 0.11;
    positions[node.id] = {x: Math.cos(angle) * radius * ring, y: Math.sin(angle) * radius * ring};
  });
  return positions;
}

function normalizeCoordinates(raw: Record<string, application.LayoutCoordinatesDTO> | undefined): Coordinates {
  const coordinates: Coordinates = {};
  for (const [id, value] of Object.entries(raw || {})) {
    if (Number.isFinite(value.x) && Number.isFinite(value.y)) {
      coordinates[id] = {x: value.x, y: value.y};
    }
  }
  return coordinates;
}

function normalizeGraphPath(value: string): string {
  return value.trim().replace(/^\/+/, '').replace(/\\/g, '/').replace(/\.md$/i, '');
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === 'string') {
    return err;
  }
  try {
    return JSON.stringify(err);
  } catch {
    return 'Unexpected error';
  }
}

export default GraphPanel;






