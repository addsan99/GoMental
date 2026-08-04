// Interactive force-directed graph (Sigma.js) with a right-hand options pane.
// The graph centres on the selected note (Neighborhood at the chosen depth) and
// falls back to the FullGraph when nothing is selected. Nodes can be dragged to
// reposition, single-clicked to select (re-centres the local graph) and
// double-clicked to open the note in the Note tab. The options pane controls
// depth, which link types are drawn, node size and the spacing between nodes.
import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import Graph from 'graphology';
import FA2Layout from 'graphology-layout-forceatlas2/worker';
import Sigma from 'sigma';
import type {application} from '../../wailsjs/go/models';
import {FullGraph, LoadGraphLayout, Neighborhood, SaveGraphLayout} from '../transport';
import {application as models} from '../../wailsjs/go/models';

type GraphViewProps = {
  workspaceOpen: boolean;
  selectedID: string;
  refreshKey: number;
  notes: application.NoteSummaryDTO[];
  theme: 'light' | 'dark';
  // Whether the graph tab is currently visible. The component stays mounted when
  // hidden (to preserve settings, camera and layout across tab swaps), so it uses
  // this to skip background work while hidden and to repaint when shown again.
  active: boolean;
  // Note IDs matching the current search query (empty when there is no query).
  // Used to spotlight matching nodes in the graph.
  searchMatchIds: string[];
  searchActive: boolean;
  onSelectNote: (id: string) => void;
  onOpenNote: (id: string) => void;
  onError: (message: string) => void;
  onStats: (stats: {notes: number; links: number}) => void;
};

type LinkType = 'hard' | 'soft' | 'metadata';

type Coordinates = Record<string, {x: number; y: number}>;

type GraphNodeAttributes = {
  x: number;
  y: number;
  size: number;
  baseSize: number;
  label: string;
  color: string;
  kind: string;
  noteId?: string;
  degree?: number;
  selected?: boolean;
  hidden?: boolean;
  highlighted?: boolean;
  forceLabel?: boolean;
};

type GraphEdgeAttributes = {
  size: number;
  baseSize: number;
  color: string;
  kind: string;
  linkType: LinkType;
  hidden?: boolean;
  // weight is the live force-layout pull (0 when the link type is hidden);
  // baseWeight is the intrinsic pull for the link type, restored when shown.
  weight?: number;
  baseWeight?: number;
};

type LayoutWorkerResponse = {
  type: 'layout';
  coordinates: Coordinates;
};

type GraphStatus = 'idle' | 'loading' | 'layout' | 'ready' | 'error';

const DEPTH_OPTIONS = [1, 2, 3];
const CLICK_DELAY = 240;

// Above this node count we skip the live, continuously-animated layout (which
// keeps a worker running and repaints every frame) and fall back to a single
// one-shot layout pass, so large graphs stay responsive. The layout settles
// freely; the camera keeps the selected node centred (see centerOnSelected).
const LIVE_LAYOUT_MAX_NODES = 250;
// Hover "breathing": the hovered node gently pulses in size. Only runs while a
// node is hovered, and only on graphs small enough for per-frame repaints.
const BREATHE_PERIOD_MS = 1500;
const BREATHE_AMPLITUDE = 0.14;
// Radial "bloom" on load: nodes start clustered at the centre and unfurl outward
// along their final bearings, settling with a slight elastic overshoot
// (Obsidian-like). This is purely a positional animation played over an
// already-settled layout — no continuous simulation. Skipped on large graphs
// where per-frame repaints would be too costly.
const BLOOM_MS = 820;
const BLOOM_START_SCALE = 0.05;

function edgeLinkType(kind: string): LinkType {
  if (kind === 'inferred_related_to') {
    return 'soft';
  }
  if (kind === 'tagged_with' || kind === 'shared_type' || kind === 'shared_heading') {
    return 'metadata';
  }
  return 'hard';
}

// Metadata facet hub node kinds (tag/type/heading), emitted by the backend.
const HUB_NODE_KINDS = new Set(['tag', 'type', 'heading']);

// Link palette: hard = indigo/blue, soft = amber, metadata = magenta. Blue and
// amber sit opposite on the wheel so hard vs soft stay easy to tell apart (and
// remain distinguishable for the common red-green colour-blindness).
function edgeColor(type: LinkType, theme: 'light' | 'dark'): string {
  const dark = theme === 'dark';
  switch (type) {
    case 'soft':
      return dark ? '#e0a54e' : '#d98a1f';
    case 'metadata':
      return dark ? '#c47bd6' : '#b054c4';
    default:
      return dark ? '#6f8dff' : '#4c63e6';
  }
}

// Restrained palette actually drawn on the canvas: desaturated and translucent
// so links read as airy hairlines and overlapping links build a soft sense of
// density, letting the node clusters (not the wiring) carry the composition.
// The solid edgeColor above is kept for the legend swatches.
function edgeRenderColor(type: LinkType, theme: 'light' | 'dark'): string {
  const dark = theme === 'dark';
  switch (type) {
    case 'soft':
      return dark ? 'rgba(206,168,96,0.24)' : 'rgba(188,146,74,0.26)';
    case 'metadata':
      return dark ? 'rgba(190,132,208,0.24)' : 'rgba(168,110,190,0.26)';
    default:
      return dark ? 'rgba(126,150,235,0.30)' : 'rgba(96,116,206,0.26)';
  }
}

function nodeColor(kind: string, selected: boolean): string {
  if (selected) {
    return '#e0603f';
  }
  if (HUB_NODE_KINDS.has(kind)) {
    return '#b054c4'; // metadata hub nodes share the metadata edge hue
  }
  return kind === 'unresolved' ? '#8b8170' : '#2f8f9e';
}

// Themed replacement for Sigma's default hover renderer, whose hard-coded white
// box leaves the light label text unreadable in dark mode. Draws a rounded pill
// to the right of the node using surface/border/text colours for the theme.
function makeHoverRenderer(getTheme: () => 'light' | 'dark') {
  return (
    context: CanvasRenderingContext2D,
    data: {x: number; y: number; size: number; label?: string | null; color: string},
    settings: {labelSize: number; labelFont: string; labelWeight: string},
  ) => {
    const dark = getTheme() === 'dark';
    const size = settings.labelSize;
    context.font = `${settings.labelWeight} ${size}px ${settings.labelFont}`;
    const label = typeof data.label === 'string' ? data.label : '';

    const padX = 8;
    const padY = 5;
    const boxHeight = Math.round(size + 2 * padY);
    const textX = data.x + data.size + 6;

    // Soft glow ring around the node — gives hovered and selected nodes a lively
    // "alive" halo without a custom WebGL program.
    context.save();
    context.beginPath();
    context.arc(data.x, data.y, data.size + 4, 0, Math.PI * 2);
    context.closePath();
    context.lineWidth = 3;
    context.strokeStyle = data.color;
    context.globalAlpha = 0.35;
    context.shadowBlur = 14;
    context.shadowColor = data.color;
    context.stroke();
    context.restore();

    context.save();
    context.fillStyle = dark ? '#1e1e26' : '#fffefb';
    context.strokeStyle = dark ? '#34343f' : '#d6d1c5';
    context.lineWidth = 1;
    context.shadowOffsetX = 0;
    context.shadowOffsetY = 2;
    context.shadowBlur = 12;
    context.shadowColor = dark ? 'rgba(0,0,0,0.55)' : 'rgba(38,35,30,0.16)';

    if (label) {
      const textWidth = context.measureText(label).width;
      const boxX = data.x + data.size;
      const boxWidth = Math.round(textWidth + padX * 2);
      roundedRect(context, boxX, data.y - boxHeight / 2, boxWidth, boxHeight, 7);
      context.fill();
      context.stroke();

      context.shadowColor = 'transparent';
      context.shadowBlur = 0;
      context.fillStyle = dark ? '#eceaf2' : '#26232b';
      context.fillText(label, textX, data.y + size / 3);
    } else {
      context.beginPath();
      context.arc(data.x, data.y, data.size + 3, 0, Math.PI * 2);
      context.closePath();
      context.fill();
    }
    context.restore();
  };
}

function roundedRect(context: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  const radius = Math.min(r, h / 2, w / 2);
  context.beginPath();
  context.moveTo(x + radius, y);
  context.arcTo(x + w, y, x + w, y + h, radius);
  context.arcTo(x + w, y + h, x, y + h, radius);
  context.arcTo(x, y + h, x, y, radius);
  context.arcTo(x, y, x + w, y, radius);
  context.closePath();
}

export function GraphView({workspaceOpen, selectedID, refreshKey, notes, theme, active, searchMatchIds, searchActive, onSelectNote, onOpenNote, onError, onStats}: GraphViewProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const sigmaRef = useRef<Sigma<GraphNodeAttributes, GraphEdgeAttributes> | null>(null);
  const graphRef = useRef<Graph<GraphNodeAttributes, GraphEdgeAttributes> | null>(null);
  const workerRef = useRef<Worker | null>(null);
  const liveLayoutRef = useRef<FA2Layout | null>(null);
  const liveStopTimerRef = useRef<number | null>(null);
  const layoutRef = useRef<Coordinates>({});
  const requestRef = useRef(0);
  const prevDistanceRef = useRef(1);
  const clickTimerRef = useRef<number | null>(null);
  const draggedNodeRef = useRef<string | null>(null);
  const movedRef = useRef(false);
  const hoveredNodeRef = useRef('');
  // A reload is deferred while the tab is hidden; this marks that inputs changed
  // and the graph must reload the next time the tab becomes visible.
  const pendingReloadRef = useRef(true);
  // Hover "breathing" + load "entrance" animations, driven by short rAF loops
  // that feed multipliers the node reducer reads. Kept in refs so the reducer
  // stays cheap and the loops don't re-render React.
  const breatheRafRef = useRef<number | null>(null);
  const breathePhaseRef = useRef(0);
  const entranceRafRef = useRef<number | null>(null);
  const entranceRef = useRef(1);

  const [depth, setDepth] = useState(2);
  // Soft (inferred/similarity) links are off by default: they are many-to-many
  // and, if drawn, mesh everything together and prevent clusters from
  // separating. They remain one click away in the options pane.
  const [linkTypes, setLinkTypes] = useState<Record<LinkType, boolean>>({hard: true, soft: false, metadata: false});
  const [nodeSize, setNodeSize] = useState(1);
  const [nodeDistance, setNodeDistance] = useState(1);
  const [focusMatches, setFocusMatches] = useState(true);
  const [ambientMotion, setAmbientMotion] = useState(false);
  const [status, setStatus] = useState<GraphStatus>('idle');
  const [message, setMessage] = useState('Open a workspace to inspect the graph.');
  const [hoveredNode, setHoveredNode] = useState('');

  // Note IDs matching the current search, as a Set for O(1) reducer lookups.
  const searchMatchSet = useMemo(() => new Set(searchMatchIds), [searchMatchIds]);

  // Mirror control state into refs so Sigma callbacks and the async loader read
  // current values without being torn down and rebuilt on every change.
  const nodeSizeRef = useRef(nodeSize);
  const nodeDistanceRef = useRef(nodeDistance);
  const linkTypesRef = useRef(linkTypes);
  const themeRef = useRef(theme);
  const activeRef = useRef(active);
  const ambientMotionRef = useRef(ambientMotion);
  const searchMatchRef = useRef(searchMatchSet);
  const searchActiveRef = useRef(searchActive);
  const focusMatchesRef = useRef(focusMatches);
  const selectedIDRef = useRef(selectedID);
  nodeSizeRef.current = nodeSize;
  nodeDistanceRef.current = nodeDistance;
  linkTypesRef.current = linkTypes;
  themeRef.current = theme;
  activeRef.current = active;
  ambientMotionRef.current = ambientMotion;
  searchMatchRef.current = searchMatchSet;
  searchActiveRef.current = searchActive;
  focusMatchesRef.current = focusMatches;
  selectedIDRef.current = selectedID;
  hoveredNodeRef.current = hoveredNode;

  // A node is spotlit by search when a query is active, "focus matches" is on,
  // and the node's note is not among the matches → it gets dimmed.
  const dimmedBySearch = useCallback((noteId: string | undefined): boolean => {
    if (!searchActiveRef.current || !focusMatchesRef.current) {
      return false;
    }
    return !noteId || !searchMatchRef.current.has(noteId);
  }, []);

  const noteIDForNode = useCallback((node: string): string => {
    const graph = graphRef.current;
    if (!graph?.hasNode(node)) {
      return '';
    }
    return graph.getNodeAttribute(node, 'noteId') || '';
  }, []);

  const applySizes = useCallback(() => {
    const graph = graphRef.current;
    if (!graph) {
      return;
    }
    const mult = nodeSizeRef.current;
    graph.forEachNode((node, attrs) => {
      graph.setNodeAttribute(node, 'size', attrs.baseSize * mult);
    });
    sigmaRef.current?.refresh();
  }, []);

  const applyDistance = useCallback((target: number) => {
    const graph = graphRef.current;
    if (!graph) {
      prevDistanceRef.current = target;
      return;
    }
    const factor = target / prevDistanceRef.current;
    prevDistanceRef.current = target;
    if (factor !== 1 && Number.isFinite(factor)) {
      graph.forEachNode((node, attrs) => {
        graph.mergeNodeAttributes(node, {x: attrs.x * factor, y: attrs.y * factor});
      });
      sigmaRef.current?.refresh();
    }
  }, []);

  // Snap the camera onto the selected note. The layout is left free (no node is
  // anchored), so this is purely a camera move: we read the selected node's
  // settled position and centre the viewport on it. We snap (no camera
  // animation) so the only motion the user sees is the graph blooming/settling.
  const centerOnSelected = useCallback(() => {
    const renderer = sigmaRef.current;
    const graph = graphRef.current;
    const camera = renderer?.getCamera();
    if (!renderer || !graph || !camera) {
      return;
    }
    if (!selectedID) {
      camera.animatedReset({duration: 0});
      return;
    }
    let target: string | null = null;
    graph.forEachNode((node, attrs) => {
      if (target === null && (attrs.noteId === selectedID || node === selectedID)) {
        target = node;
      }
    });
    if (target === null) {
      camera.animatedReset({duration: 0});
      return;
    }
    renderer.refresh();
    const pos = renderer.getNodeDisplayData(target);
    if (!pos) {
      return;
    }
    camera.setState({x: pos.x, y: pos.y});
  }, [selectedID]);

  const applyVisibility = useCallback(() => {
    const graph = graphRef.current;
    if (!graph) {
      return;
    }
    const selected = linkTypesRef.current;
    let visibleLinks = 0;
    graph.forEachEdge((edge, attrs) => {
      const visible = selected[attrs.linkType];
      graph.setEdgeAttribute(edge, 'hidden', !visible);
      // Hidden links exert no pull on the force layout, so the layout reflects
      // only the visible link types — this is what lets clusters separate.
      graph.setEdgeAttribute(edge, 'weight', visible ? (attrs.baseWeight ?? 1) : 0);
      if (visible) {
        visibleLinks += 1;
      }
    });
    sigmaRef.current?.refresh();
    onStats({notes: graph.order, links: visibleLinks});
  }, [onStats]);

  // Hover "breathing": a short rAF loop that pulses the hovered node's size.
  // Only one node is ever affected, and it is skipped on large graphs where a
  // per-frame repaint would be too costly.
  const stopBreathing = useCallback(() => {
    if (breatheRafRef.current !== null) {
      cancelAnimationFrame(breatheRafRef.current);
      breatheRafRef.current = null;
    }
    breathePhaseRef.current = 0;
  }, []);

  const startBreathing = useCallback(() => {
    const graph = graphRef.current;
    if (!graph || graph.order > LIVE_LAYOUT_MAX_NODES || breatheRafRef.current !== null) {
      return;
    }
    let last = 0;
    const step = (ts: number) => {
      const dt = last === 0 ? 16 : ts - last;
      last = ts;
      breathePhaseRef.current += (dt / BREATHE_PERIOD_MS) * Math.PI * 2;
      sigmaRef.current?.refresh({skipIndexation: true});
      breatheRafRef.current = requestAnimationFrame(step);
    };
    breatheRafRef.current = requestAnimationFrame(step);
  }, []);

  const mountSigma = useCallback(() => {
    const graph = graphRef.current;
    const container = containerRef.current;
    if (!graph || !container) {
      return;
    }
    if (sigmaRef.current) {
      sigmaRef.current.setSetting('labelColor', {color: themeRef.current === 'dark' ? '#eceaf2' : '#26232b'});
      // Drop any frozen bloom frame from the previous graph so the fresh graph
      // frames naturally until its own settled extent is computed.
      sigmaRef.current.setCustomBBox(null);
      sigmaRef.current.setGraph(graph);
      sigmaRef.current.refresh();
      return;
    }
    const renderer = new Sigma<GraphNodeAttributes, GraphEdgeAttributes>(graph, container, {
      allowInvalidContainer: true,
      defaultEdgeType: 'line',
      defaultNodeColor: '#2f8f9e',
      defaultEdgeColor: 'rgba(140,140,150,0.20)',
      renderEdgeLabels: false,
      // Restrained labelling: only the larger nodes (hubs, the selected note)
      // auto-label, so the canvas stays uncluttered and node shape reads first;
      // smaller nodes reveal their labels on hover or as you zoom in.
      labelRenderedSizeThreshold: 5,
      labelDensity: 0.6,
      labelColor: {color: themeRef.current === 'dark' ? '#eceaf2' : '#26232b'},
      labelSize: 12,
      labelFont: 'Segoe UI, Arial, sans-serif',
      labelWeight: 'normal',
      defaultDrawNodeHover: makeHoverRenderer(() => themeRef.current),
      enableEdgeEvents: true,
      nodeReducer: (node, data) => {
        const g = graphRef.current;
        // Guard against a stale hover after a graph reload: mountSigma swaps a
        // fresh graph into the renderer while hoveredNodeRef may still point at a
        // node from the previous graph. areNeighbors throws on unknown nodes.
        if (!g || !g.hasNode(node)) {
          return data;
        }
        const dark = themeRef.current === 'dark';
        const res = {...data};

        // Bloom scale-in: nodes ease up to full size as they unfurl outward.
        const entrance = entranceRef.current;
        if (entrance < 1) {
          res.size = res.size * (0.4 + 0.6 * entrance);
        }

        const hovered = hoveredNodeRef.current;
        const hoverActive = Boolean(hovered) && g.hasNode(hovered);
        const isHovered = hoverActive && node === hovered;
        const isSelected = Boolean(data.selected);

        // Selected + hovered nodes get the persistent halo (drawn by the hover
        // renderer) and always keep their label.
        if (isSelected || isHovered) {
          res.highlighted = true;
          res.forceLabel = true;
        }

        // Hovered node pops and gently breathes.
        if (isHovered) {
          const breathe = 1 + BREATHE_AMPLITUDE * Math.sin(breathePhaseRef.current);
          res.size = res.size * 1.25 * breathe;
          return res;
        }

        // Fade rules (selected never fades): dim non-matches while searching, and
        // dim non-neighbours of the hovered node.
        let fade = false;
        if (!isSelected) {
          if (dimmedBySearch(data.noteId)) {
            fade = true;
          } else if (hoverActive && !g.areNeighbors(node, hovered)) {
            fade = true;
          }
        }
        if (fade) {
          res.color = dark ? '#3a4247' : '#d8d2c4';
          res.label = '';
        } else if (searchActiveRef.current && focusMatchesRef.current && data.noteId && searchMatchRef.current.has(data.noteId)) {
          // A search match: emphasise and keep its label visible.
          res.forceLabel = true;
          res.size = res.size * 1.15;
        }
        return res;
      },
      edgeReducer: (edge, data) => {
        const g = graphRef.current;
        if (!g || !g.hasEdge(edge) || data.hidden) {
          return data;
        }
        const dark = themeRef.current === 'dark';
        const res = {...data};
        const source = g.source(edge);
        const target = g.target(edge);
        const fadeColor = dark ? '#2c2f33' : '#e7e2d7';

        // Search focus: fade edges that touch no matching node.
        if (searchActiveRef.current && focusMatchesRef.current) {
          const sMatch = searchMatchRef.current.has(g.getNodeAttribute(source, 'noteId') || '');
          const tMatch = searchMatchRef.current.has(g.getNodeAttribute(target, 'noteId') || '');
          if (!sMatch && !tMatch) {
            res.color = fadeColor;
          }
        }

        // Hover: light up edges incident to the hovered node, fade the rest.
        const hovered = hoveredNodeRef.current;
        if (hovered && g.hasNode(hovered)) {
          if (source === hovered || target === hovered) {
            res.color = dark ? '#8aa2ff' : '#3a52d6';
            res.size = data.baseSize * 2.4;
          } else {
            res.color = fadeColor;
          }
        }
        return res;
      },
    });

    renderer.on('clickNode', ({node}) => {
      if (movedRef.current) {
        return;
      }
      const noteID = noteIDForNode(node);
      if (!noteID) {
        return;
      }
      if (clickTimerRef.current !== null) {
        window.clearTimeout(clickTimerRef.current);
      }
      clickTimerRef.current = window.setTimeout(() => {
        clickTimerRef.current = null;
        onSelectNote(noteID);
      }, CLICK_DELAY);
    });

    renderer.on('doubleClickNode', ({node, event}) => {
      event.preventSigmaDefault();
      const noteID = noteIDForNode(node);
      if (!noteID) {
        return;
      }
      if (clickTimerRef.current !== null) {
        window.clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
      }
      onOpenNote(noteID);
    });

    renderer.on('enterNode', ({node}) => {
      setHoveredNode(node);
      startBreathing();
    });
    renderer.on('leaveNode', () => {
      setHoveredNode('');
      stopBreathing();
    });

    // Drag to move. Handlers read graphRef.current (not the closure `graph`)
    // so they keep working after a reload swaps in a fresh graph via setGraph.
    renderer.on('downNode', ({node}) => {
      const g = graphRef.current;
      if (!g?.hasNode(node)) {
        return;
      }
      draggedNodeRef.current = node;
      movedRef.current = false;
      g.setNodeAttribute(node, 'highlighted', true);
      if (!renderer.getCustomBBox()) {
        renderer.setCustomBBox(renderer.getBBox());
      }
    });
    const mouse = renderer.getMouseCaptor();
    mouse.on('mousemovebody', (event) => {
      const dragged = draggedNodeRef.current;
      const g = graphRef.current;
      if (!dragged || !g?.hasNode(dragged)) {
        return;
      }
      movedRef.current = true;
      const pos = renderer.viewportToGraph(event);
      g.setNodeAttribute(dragged, 'x', pos.x);
      g.setNodeAttribute(dragged, 'y', pos.y);
      event.preventSigmaDefault();
      event.original.preventDefault();
      event.original.stopPropagation();
    });
    const endDrag = () => {
      const dragged = draggedNodeRef.current;
      const g = graphRef.current;
      if (dragged && g?.hasNode(dragged)) {
        g.removeNodeAttribute(dragged, 'highlighted');
      }
      draggedNodeRef.current = null;
      // Clear the moved flag after the click event has had a chance to fire so
      // a drag is never mistaken for a selecting click.
      window.setTimeout(() => {
        movedRef.current = false;
      }, 0);
    };
    mouse.on('mouseup', endDrag);
    sigmaRef.current = renderer;
  }, [noteIDForNode, onSelectNote, onOpenNote, startBreathing, stopBreathing, dimmedBySearch]);

  // Snapshot the current node positions and persist them.
  const saveLayoutFromGraph = useCallback(() => {
    const graph = graphRef.current;
    if (!graph) {
      return;
    }
    const coordinates: Coordinates = {};
    graph.forEachNode((id, attrs) => {
      coordinates[id] = {
        x: Number.isFinite(attrs.x) ? attrs.x : 0,
        y: Number.isFinite(attrs.y) ? attrs.y : 0,
      };
    });
    layoutRef.current = coordinates;
    void SaveGraphLayout(models.LayoutSnapshotDTO.createFrom({coordinates, updatedAt: new Date().toISOString()}))
      .catch((err) => onError(errorMessage(err)));
  }, [onError]);

  // Tear down the live (continuous) layout, optionally persisting where it
  // settled. Safe to call when nothing is running.
  const stopLiveLayout = useCallback((save: boolean) => {
    if (liveStopTimerRef.current !== null) {
      window.clearTimeout(liveStopTimerRef.current);
      liveStopTimerRef.current = null;
    }
    if (liveLayoutRef.current) {
      liveLayoutRef.current.kill();
      liveLayoutRef.current = null;
      if (save) {
        saveLayoutFromGraph();
      }
    }
  }, [saveLayoutFromGraph]);

  // Spawn a fresh continuously-animated ForceAtlas2 layout (option B). No node is
  // anchored — the graph drifts freely; the caller schedules an auto-stop when
  // not in ambient mode.
  const startLiveLayout = useCallback((): boolean => {
    const graph = graphRef.current;
    if (!graph || graph.order > LIVE_LAYOUT_MAX_NODES) {
      return false;
    }
    stopLiveLayout(false);
    // Let live motion re-frame itself rather than staying pinned to the frozen
    // bloom extent.
    sigmaRef.current?.setCustomBBox(null);
    try {
      const supervisor = new FA2Layout(graph, {
        settings: {
          // Match the one-shot worker: LinLog + hub-dissuasion for clustered,
          // dandelion-style layouts; edge weights so hidden links exert no pull.
          linLogMode: true,
          outboundAttractionDistribution: true,
          adjustSizes: true,
          barnesHutOptimize: graph.order > 80,
          gravity: 1.0,
          scalingRatio: 3,
          edgeWeightInfluence: 1,
          slowDown: 5,
        },
      });
      liveLayoutRef.current = supervisor;
      supervisor.start();
      return true;
    } catch {
      // Blob web worker unavailable — caller falls back to the one-shot layout.
      liveLayoutRef.current = null;
      return false;
    }
  }, [stopLiveLayout]);

  // Radial bloom: play a positional animation over the already-settled layout so
  // nodes unfurl outward from the centre (the layout barycentre) along their
  // final bearings. Every node blooms uniformly — none is anchored.
  // No continuous simulation — this runs once per load and then stops.
  const playBloom = useCallback((targets: Coordinates, done?: () => void) => {
    if (entranceRafRef.current !== null) {
      cancelAnimationFrame(entranceRafRef.current);
      entranceRafRef.current = null;
    }
    const graph = graphRef.current;
    if (!graph) {
      entranceRef.current = 1;
      done?.();
      return;
    }
    // Large graphs: skip the per-frame bloom; the settled positions are already
    // in place, just paint them.
    if (graph.order > LIVE_LAYOUT_MAX_NODES) {
      entranceRef.current = 1;
      sigmaRef.current?.refresh();
      done?.();
      return;
    }

    // Compress every node toward the centre along its final bearing.
    graph.forEachNode((id) => {
      const tgt = targets[id];
      if (tgt) {
        graph.mergeNodeAttributes(id, {x: tgt.x * BLOOM_START_SCALE, y: tgt.y * BLOOM_START_SCALE});
      }
    });
    entranceRef.current = 0;
    sigmaRef.current?.refresh({skipIndexation: true});

    let start = 0;
    const step = (ts: number) => {
      if (start === 0) {
        start = ts;
      }
      const t = Math.min(1, (ts - start) / BLOOM_MS);
      const scale = BLOOM_START_SCALE + (1 - BLOOM_START_SCALE) * easeOutBack(t);
      const g = graphRef.current;
      if (!g) {
        entranceRafRef.current = null;
        return;
      }
      g.forEachNode((id) => {
        const tgt = targets[id];
        if (tgt) {
          g.mergeNodeAttributes(id, {x: tgt.x * scale, y: tgt.y * scale});
        }
      });
      entranceRef.current = t;
      sigmaRef.current?.refresh({skipIndexation: true});
      if (t < 1) {
        entranceRafRef.current = requestAnimationFrame(step);
        return;
      }
      // Settle exactly on target (undo any overshoot residue).
      entranceRef.current = 1;
      entranceRafRef.current = null;
      g.forEachNode((id) => {
        const tgt = targets[id];
        if (tgt) {
          g.mergeNodeAttributes(id, {x: tgt.x, y: tgt.y});
        }
      });
      sigmaRef.current?.refresh({skipIndexation: true});
      done?.();
    };
    entranceRafRef.current = requestAnimationFrame(step);
  }, []);

  const load = useCallback(async () => {
    if (!workspaceOpen) {
      setStatus('idle');
      setMessage('Open a workspace to inspect the graph.');
      layoutRef.current = {};
      return;
    }
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    // A new load supersedes any running live layout.
    stopLiveLayout(false);
    setStatus('loading');
    try {
      const [dto, savedLayout] = await Promise.all([
        selectedID
          ? Neighborhood(selectedID, depth)
          : FullGraph({pathPrefix: '', tags: [], favoritesOnly: false, includeUnresolved: false, includeSoftLinks: true, includeMetadataLinks: linkTypes.metadata, depth: 0}),
        LoadGraphLayout().catch(() => models.LayoutSnapshotDTO.createFrom({coordinates: {}})),
      ]);
      if (requestRef.current !== requestID) {
        return;
      }
      layoutRef.current = normalizeCoordinates(savedLayout.coordinates);
      const graph = buildGraph(dto, selectedID, layoutRef.current, themeRef.current);
      graphRef.current = graph;
      mountSigma();
      // Apply current control values to the freshly built graph.
      prevDistanceRef.current = 1;
      applySizes();
      applyDistance(nodeDistanceRef.current);
      applyVisibility();

      // The layout settles freely — no node is anchored. The camera (not the
      // layout) keeps the selected note centred, so clusters find their natural
      // shape while the focus node still lands in the middle of the viewport.
      centerOnSelected();

      // Settle the layout once in a worker, then play the radial bloom over the
      // settled positions. Continuous motion (ambient), if enabled, resumes only
      // after the bloom has finished.
      setStatus('layout');
      runLayoutWorker(graph, workerRef, async (coordinates) => {
        const g = graphRef.current;
        if (requestRef.current !== requestID || !g) {
          return;
        }
        for (const [id, point] of Object.entries(coordinates)) {
          if (g.hasNode(id)) {
            g.mergeNodeAttributes(id, {x: point.x, y: point.y});
          }
        }
        prevDistanceRef.current = 1;
        applyDistance(nodeDistanceRef.current);

        // Capture the settled (distance-scaled) positions as the bloom targets.
        const targets: Coordinates = {};
        g.forEachNode((id, attrs) => {
          targets[id] = {x: attrs.x, y: attrs.y};
        });
        layoutRef.current = targets;

        // Freeze the framing to the settled extent (with a little padding for
        // breathing room) so the bloom unfurls within a stable, calm viewport
        // instead of pushing nodes off-screen.
        const renderer = sigmaRef.current;
        renderer?.refresh();
        const bbox = renderer?.getBBox();
        if (renderer && bbox) {
          renderer.setCustomBBox(padBBox(bbox, 0.12));
        }
        centerOnSelected();
        setStatus('ready');
        playBloom(targets, () => {
          if (requestRef.current === requestID && ambientMotionRef.current) {
            startLiveLayout();
          }
        });

        try {
          await SaveGraphLayout(models.LayoutSnapshotDTO.createFrom({coordinates, updatedAt: new Date().toISOString()}));
        } catch (err) {
          onError(errorMessage(err));
        }
      });
    } catch (err) {
      if (requestRef.current !== requestID) {
        return;
      }
      const text = errorMessage(err);
      setStatus('error');
      setMessage(text);
      onError(text);
    }
  }, [workspaceOpen, selectedID, depth, linkTypes.metadata, mountSigma, applySizes, applyDistance, applyVisibility, centerOnSelected, playBloom, startLiveLayout, stopLiveLayout, onError]);

  // Reload when inputs change — but defer while the tab is hidden, then catch up
  // when it becomes visible so tab-swapping preserves graph state.
  useEffect(() => {
    pendingReloadRef.current = true;
    if (activeRef.current) {
      pendingReloadRef.current = false;
      void load();
    }
  }, [load, refreshKey]);

  // React to the tab becoming visible/hidden. Hidden: pause motion to save CPU.
  // Visible: the container had no size while hidden, so resize + repaint, then
  // run any deferred reload (or resume ambient motion).
  useEffect(() => {
    if (!active) {
      liveLayoutRef.current?.stop();
      stopBreathing();
      return;
    }
    sigmaRef.current?.resize(true);
    sigmaRef.current?.refresh();
    if (pendingReloadRef.current) {
      pendingReloadRef.current = false;
      void load();
    } else if (ambientMotionRef.current && liveLayoutRef.current && !liveLayoutRef.current.isRunning()) {
      liveLayoutRef.current.start();
    }
  }, [active, load, stopBreathing]);

  useEffect(() => () => {
    workerRef.current?.terminate();
    workerRef.current = null;
    stopLiveLayout(false);
    stopBreathing();
    if (entranceRafRef.current !== null) {
      cancelAnimationFrame(entranceRafRef.current);
      entranceRafRef.current = null;
    }
    if (clickTimerRef.current !== null) {
      window.clearTimeout(clickTimerRef.current);
      clickTimerRef.current = null;
    }
    sigmaRef.current?.kill();
    sigmaRef.current = null;
    graphRef.current = null;
  }, [stopLiveLayout, stopBreathing]);

  useEffect(() => {
    sigmaRef.current?.refresh();
  }, [hoveredNode]);

  // Repaint (no reload) when the search spotlight changes.
  useEffect(() => {
    sigmaRef.current?.refresh();
  }, [searchMatchSet, searchActive, focusMatches]);

  // Toggle perpetual "ambient" motion on demand without a full reload.
  useEffect(() => {
    if (!activeRef.current || !graphRef.current) {
      return;
    }
    if (ambientMotion) {
      if (liveStopTimerRef.current !== null) {
        window.clearTimeout(liveStopTimerRef.current);
        liveStopTimerRef.current = null;
      }
      startLiveLayout();
    } else if (liveLayoutRef.current) {
      stopLiveLayout(true);
    }
  }, [ambientMotion, startLiveLayout, stopLiveLayout]);

  useEffect(() => {
    applySizes();
  }, [nodeSize, applySizes]);

  useEffect(() => {
    applyDistance(nodeDistance);
  }, [nodeDistance, applyDistance]);

  useEffect(() => {
    applyVisibility();
  }, [linkTypes, applyVisibility]);

  const toggleLinkType = useCallback((type: LinkType) => {
    setLinkTypes((current) => ({...current, [type]: !current[type]}));
  }, []);

  const resetView = useCallback(() => {
    sigmaRef.current?.getCamera().animatedReset({duration: 220});
  }, []);

  const showOverlay = status === 'idle' || status === 'error';

  return (
    <div className="gm-graph-layout">
      <div className="gm-graph-stage">
        <div className="gm-graph-canvas gm-graph-canvas--live" ref={containerRef} />
        {showOverlay && <div className="gm-graph-empty">{message}</div>}
        {(status === 'loading' || status === 'layout') && (
          <div className="gm-graph-badge">{status === 'loading' ? 'Loading…' : 'Laying out…'}</div>
        )}
      </div>
      <aside className="gm-graph-options" aria-label="Graph options">
        <h3 className="gm-graph-options-title">Graph options</h3>

        <div className="gm-graph-opt">
          <label className="gm-graph-opt-label" htmlFor="gm-depth">
            Depth <span className="gm-graph-opt-value">{depth}</span>
          </label>
          <input
            id="gm-depth"
            type="range"
            min={DEPTH_OPTIONS[0]}
            max={DEPTH_OPTIONS[DEPTH_OPTIONS.length - 1]}
            step={1}
            value={depth}
            onChange={(event) => setDepth(Number(event.target.value))}
          />
          <div className="gm-graph-ticks">
            {DEPTH_OPTIONS.map((value) => <span key={value}>{value}</span>)}
          </div>
          <p className="gm-graph-opt-hint">Nodes within this many hops of the selected note.</p>
        </div>

        <div className="gm-graph-opt">
          <span className="gm-graph-opt-label">Link types</span>
          <label className="gm-graph-check">
            <input type="checkbox" checked={linkTypes.hard} onChange={() => toggleLinkType('hard')} />
            <span className="gm-graph-swatch" style={{background: edgeColor('hard', theme)}} />
            Hard links
          </label>
          <label className="gm-graph-check">
            <input type="checkbox" checked={linkTypes.soft} onChange={() => toggleLinkType('soft')} />
            <span className="gm-graph-swatch" style={{background: edgeColor('soft', theme)}} />
            Soft links
          </label>
          <label className="gm-graph-check">
            <input type="checkbox" checked={linkTypes.metadata} onChange={() => toggleLinkType('metadata')} />
            <span className="gm-graph-swatch" style={{background: edgeColor('metadata', theme)}} />
            Metadata
          </label>
        </div>

        <div className="gm-graph-opt">
          <span className="gm-graph-opt-label">Visualization</span>
          <label className="gm-graph-opt-label sub" htmlFor="gm-node-size">
            Node size <span className="gm-graph-opt-value">{nodeSize.toFixed(1)}×</span>
          </label>
          <input
            id="gm-node-size"
            type="range"
            min={0.5}
            max={2.5}
            step={0.1}
            value={nodeSize}
            onChange={(event) => setNodeSize(Number(event.target.value))}
          />
          <label className="gm-graph-opt-label sub" htmlFor="gm-node-distance">
            Distance <span className="gm-graph-opt-value">{nodeDistance.toFixed(1)}×</span>
          </label>
          <input
            id="gm-node-distance"
            type="range"
            min={0.4}
            max={2.5}
            step={0.1}
            value={nodeDistance}
            onChange={(event) => setNodeDistance(Number(event.target.value))}
          />
        </div>

        <div className="gm-graph-opt">
          <span className="gm-graph-opt-label">Motion &amp; focus</span>
          <label className="gm-graph-check">
            <input type="checkbox" checked={ambientMotion} onChange={() => setAmbientMotion((on) => !on)} />
            Keep nodes gently moving
          </label>
          <label className="gm-graph-check">
            <input type="checkbox" checked={focusMatches} onChange={() => setFocusMatches((on) => !on)} />
            Spotlight search matches
          </label>
          <p className="gm-graph-opt-hint">
            {searchActive
              ? `${searchMatchIds.length} note${searchMatchIds.length === 1 ? '' : 's'} match the current search.`
              : 'When searching, matching notes stay bright and the rest dim.'}
          </p>
        </div>

        <button type="button" className="gm-graph-reset" onClick={resetView} disabled={status !== 'ready'}>
          Reset view
        </button>
        <p className="gm-graph-opt-hint">Drag a node to move it. Click to select, double-click to open.</p>
      </aside>
    </div>
  );
}

function buildGraph(
  dto: application.GraphDTO,
  selectedID: string,
  saved: Coordinates,
  theme: 'light' | 'dark',
): Graph<GraphNodeAttributes, GraphEdgeAttributes> {
  const graph = new Graph<GraphNodeAttributes, GraphEdgeAttributes>({multi: true, type: 'directed'});
  const positioned = initialPositions(dto.nodes, saved);

  // Degree per node, so well-connected "hub" notes render a little larger and
  // read as more important at a glance.
  const degree: Record<string, number> = {};
  for (const edge of dto.edges) {
    degree[edge.source] = (degree[edge.source] || 0) + 1;
    degree[edge.target] = (degree[edge.target] || 0) + 1;
  }

  for (const node of dto.nodes) {
    const point = positioned[node.id] || {x: 0, y: 0};
    const noteID = node.noteId || (node.kind === 'note' ? node.id : undefined);
    const selected = Boolean(noteID && noteID === selectedID);
    const deg = degree[node.id] || 0;
    // Gentle logarithmic bump for hubs (caps out so nothing dominates).
    const hubBump = node.kind === 'unresolved' ? 0 : Math.min(5, Math.log2(deg + 1) * 1.6);
    const baseSize = selected ? 12 : (node.kind === 'unresolved' ? 5 : 7) + hubBump;
    graph.addNode(node.id, {
      x: point.x,
      y: point.y,
      size: baseSize,
      baseSize,
      label: node.label || noteID || node.id,
      color: nodeColor(node.kind, selected),
      kind: node.kind,
      noteId: noteID,
      degree: deg,
      selected,
      forceLabel: selected,
    });
  }

  for (const edge of dto.edges) {
    if (!graph.hasNode(edge.source) || !graph.hasNode(edge.target)) {
      continue;
    }
    const linkType = edgeLinkType(edge.kind);
    const size = linkType === 'soft' ? 0.8 : 1.1;
    // Layout pull: hard links pull hard to form tight clusters; soft/metadata
    // links are weak springs so cross-links don't collapse clusters together.
    const baseWeight = linkType === 'hard' ? 1 : linkType === 'soft' ? 0.15 : 0.1;
    graph.addDirectedEdgeWithKey(edge.id || `${edge.source}->${edge.target}-${edge.kind}`, edge.source, edge.target, {
      size,
      baseSize: size,
      color: edgeRenderColor(linkType, theme),
      kind: edge.kind,
      linkType,
      weight: baseWeight,
      baseWeight,
    });
  }

  return graph;
}

function runLayoutWorker(
  graph: Graph<GraphNodeAttributes, GraphEdgeAttributes>,
  workerRef: React.MutableRefObject<Worker | null>,
  onLayout: (coordinates: Coordinates) => void | Promise<void>,
) {
  workerRef.current?.terminate();
  const worker = new Worker(new URL('../graphLayoutWorker.ts', import.meta.url), {type: 'module'});
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
      size: graph.getNodeAttribute(id, 'baseSize'),
    })),
    edges: graph
      .edges()
      // Hidden link types don't participate in the layout, so clusters reflect
      // only the visible structure.
      .filter((id) => !graph.getEdgeAttribute(id, 'hidden'))
      .map((id) => ({
        id,
        source: graph.source(id),
        target: graph.target(id),
        weight: graph.getEdgeAttribute(id, 'weight') || 1,
      })),
    // LinLog needs more passes to settle into separated clusters than the old
    // linear model did.
    iterations: graph.order > 120 ? 300 : 450,
  });
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
    if (value && Number.isFinite(value.x) && Number.isFinite(value.y)) {
      coordinates[id] = {x: value.x, y: value.y};
    }
  }
  return coordinates;
}

// Ease-out with a gentle elastic overshoot, so blooming nodes push a touch past
// their resting position and settle back — reads as "alive" rather than robotic.
// Returns exactly 0 at t=0 and exactly 1 at t=1.
function easeOutBack(t: number): number {
  const c1 = 1.70158;
  const c3 = c1 + 1;
  return 1 + c3 * Math.pow(t - 1, 3) + c1 * Math.pow(t - 1, 2);
}

// Grow a Sigma bounding box outward by a ratio on each axis, leaving whitespace
// around the settled graph. Handles both the [min, max] tuple and {min, max}
// object shapes Sigma may report, returning the same shape it received.
function padBBox<T extends {x: unknown; y: unknown}>(bbox: T, ratio: number): T {
  const grow = (extent: unknown): unknown => {
    if (Array.isArray(extent)) {
      const [min, max] = extent as [number, number];
      const pad = (max - min) * ratio;
      return [min - pad, max + pad];
    }
    const e = extent as {min: number; max: number};
    const pad = (e.max - e.min) * ratio;
    return {min: e.min - pad, max: e.max + pad};
  };
  return {x: grow(bbox.x), y: grow(bbox.y)} as unknown as T;
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

export default GraphView;
