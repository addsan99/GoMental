// Interactive graph view (Three.js via react-force-graph-3d) with a right-hand
// options pane. A single renderer backs two lenses: a flat, top-down 2D map and a
// free-orbit 3D scene. What is shown is driven by a GraphViewState (seed + depth +
// facet filters + link types + layout), sent to the backend via GraphQuery.
//
// Three layout strategies are available (force / radial / zoned). Force is the
// live d3 simulation; radial and zoned are deterministic engines whose positions
// are pinned onto the nodes (fx/fy). Zoned additionally draws rounded hulls around
// each group, rendered as translucent Three.js shapes behind the nodes (flat mode).
//
// Facet filters compose with the seed as "focus + context": non-matching nodes are
// dimmed and shrunk rather than removed, unless "Hide non-matches" is on. Selecting
// a note gently re-centres the camera on it (no settle-wait jump).
import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import ForceGraph3D from 'react-force-graph-3d';
import type {ForceGraphMethods, NodeObject, LinkObject} from 'react-force-graph-3d';
import SpriteText from 'three-spritetext';
import {forceRadial, forceX, forceY} from 'd3-force-3d';
import {Color, DoubleSide, Mesh, MeshBasicMaterial, MOUSE, Object3D, Shape, ShapeGeometry, SpriteMaterial} from 'three';
import {application as models} from '../../wailsjs/go/models';
import {GraphQuery, LoadGraphLayout, SaveGraphLayout} from '../transport';
import type {GraphData, GraphLink, GraphNode, LinkType} from './graph/model';
import {applySavedLayout, buildData} from './graph/model';
import {
  DEPTH_OPTIONS,
  HUB_NODE_KINDS,
  MAX_DEPTH_HOPS,
  backgroundColor,
  depthShade,
  edgeColor,
  labelColor,
  noteShade,
  nodeColor,
} from './graph/palette';
import {reportStats} from './graph/stats';
import {layoutEngineFor} from './graph/layout';
import type {Hull, LayoutEdge, LayoutNode} from './graph/layout';
import {GraphFilterPanel, defaultGraphViewState, toGraphQueryDTO, folderOf, facetMatchesNote} from './graph/filters';
import type {GraphViewState, FacetFilter} from './graph/filters';
import {errorMessage} from '../util';

type GraphView3DProps = {
  workspaceOpen: boolean;
  selectedID: string;
  refreshKey: number;
  theme: 'light' | 'dark';
  // The note summaries backing the sidebar — used to derive the available facet
  // options (types / tags / folders) and to join graph nodes to their metadata
  // for grouping (zoned layout) and facet matching. GraphNodeDTO itself carries
  // only id/label/kind, so metadata comes from here.
  notes: models.NoteSummaryDTO[];
  // Facet selection (Types / Tags / Folders) is owned by App and shared with the
  // note-list filter, so the same chips narrow both views. The panel that edits
  // these now lives in the right rail; the graph consumes them read-mostly, and
  // calls onFacetsChange when a super-node is drilled into.
  facets: FacetFilter;
  onFacetsChange: (facets: FacetFilter) => void;
  // Whether the graph tab is currently visible AND this lens (2D/3D) is selected.
  // The component stays mounted when hidden (to preserve camera + settings across
  // swaps), so it uses this to pause the render loop while hidden and to reload
  // any deferred change when shown again.
  active: boolean;
  // Flat mode renders the same lit spheres/labels/particles but locks the camera
  // top-down with rotation disabled (a "flat 3D" 2D view). Only the flat instance
  // reads/writes the persisted {x,y} layout, and only for the force layout; the
  // deterministic engines compute positions fresh each time.
  flat?: boolean;
  // Depth (hops from the seed) is owned by the parent so the setting is shared
  // between the 2D and 3D instances — switching modes keeps the same depth.
  depth: number;
  onDepthChange: (depth: number) => void;
  searchMatchIds: string[];
  searchActive: boolean;
  onSelectNote: (id: string) => void;
  onOpenNote: (id: string) => void;
  onError: (message: string) => void;
  onStats: (stats: {notes: number; links: number}) => void;
};

type FGNode = NodeObject<GraphNode>;
type FGLink = LinkObject<GraphNode, GraphLink>;
type FGMethods = ForceGraphMethods<GraphNode, GraphLink>;

type GraphStatus = 'idle' | 'loading' | 'ready' | 'error';

const CLICK_DELAY = 240;
// How far (in graph units) to sit the camera from a node when flying to it.
const CAMERA_DISTANCE = 160;
// Below this node count every node is labelled. Above it, labels are limited to
// the selected node, hubs, and well-connected notes (degree ≥ DEGREE_LABEL_MIN)
// so a dense graph isn't buried under hundreds of overlapping text sprites.
const LABEL_ALL_MAX_NODES = 90;
const DEGREE_LABEL_MIN = 6;
// Camera dolly clamps (graph units), shared by flat + orbit. Keeps a single node
// from filling the viewport and the graph from being flung out of sight.
const ZOOM_MIN_DISTANCE = 45;
const ZOOM_MAX_DISTANCE = 4000;
// Directional particles animated along the links touching the selected node.
const SELECTED_PARTICLES = 4;
// Label sizes. Ordinary/hub labels are world-space (three-spritetext textHeight in
// world units) so they scale with zoom like the nodes. The selected-note and zone
// labels are instead SCREEN-FIXED (sprite sizeAttenuation off) so they stay legible
// at any zoom, like the hover tooltip — for those, textHeight is roughly a fraction
// of the viewport height, hence the tiny values. All four are tuning knobs.
const NODE_LABEL_HEIGHT = 4;
const HUB_LABEL_HEIGHT = 4.8;
const SELECTED_LABEL_HEIGHT = 0.017;
const ZONE_LABEL_HEIGHT = 0.017;
// Base d3-force spacing, scaled by the Distance slider. The charge:link ratio is
// deliberately high so well-connected hubs splay their leaves into distinct
// "dandelion" bursts (the organic force look) rather than a uniform mesh.
const BASE_CHARGE = -120;
const BASE_LINK_DISTANCE = 40;
// Radial layout: base gap between hop-rings (scaled by Distance) and how firmly
// nodes are pulled onto their ring. A moderate strength gives soft, organic bands
// keyed to distance-from-focus rather than the rigid concentric rings of the old
// deterministic radial engine.
const RADIAL_RING_GAP = 55;
const RADIAL_STRENGTH = 0.7;
// The selected node is usually a hub; at full charge it repels its many neighbours
// into a wide halo, leaving it marooned in empty space. Damping its own repulsion
// to near zero lets neighbours settle close around it (they still repel each other,
// so they don't overlap). Applies in force + radial.
const SELECTED_CHARGE_SCALE = 0.08;
// Radial already positions every node on a hop-distance ring, so strong global
// repulsion only pushes nodes off their rings (inflating the centre void and
// exaggerating the "dandelion" splay). Damp the charge hard in radial so nodes hug
// their rings; force keeps full charge for its organic spread.
const RADIAL_CHARGE_SCALE = 0.2;
// Gentle pull toward the origin in the force layout so disconnected components
// don't drift far apart and the graph doesn't elongate along one diagonal.
const CENTER_STRENGTH = 0.045;
// Sphere volume of the selected node, so a click-selected node grows even when no
// reload rebuilds it (matches the value buildData assigns on load).
const SELECTED_VAL = 18;
// Hard cap on rendered nodes. Above this the most-connected nodes (plus the
// selected one) are kept and the remainder is summarised as an "N more" badge, so
// a 10K-node query still renders smoothly instead of locking up the sim.
const NODE_RENDER_CAP = 2500;
// Above this many nodes, an UNFOCUSED graph collapses into one super-node per
// group (folder/type/tag) — an overview you drill into by clicking a group, which
// narrows the query via the matching facet. Focused (seeded) graphs are bounded by
// depth and never aggregate.
const AGGREGATE_THRESHOLD = 1500;
// Which facet axis a group-by drills into when an aggregate super-node is clicked.
const GROUPBY_AXIS: Record<string, 'types' | 'tags' | 'folders'> = {type: 'types', tag: 'tags', folder: 'folders'};

export function GraphView3D({
  workspaceOpen,
  selectedID,
  refreshKey,
  theme,
  notes,
  facets,
  onFacetsChange,
  active,
  flat = false,
  depth,
  onDepthChange,
  searchMatchIds,
  searchActive,
  onSelectNote,
  onOpenNote,
  onError,
  onStats,
}: GraphView3DProps) {
  const stageRef = useRef<HTMLDivElement | null>(null);
  const fgRef = useRef<FGMethods | undefined>(undefined);
  const requestRef = useRef(0);
  const clickTimerRef = useRef<number | null>(null);
  // A reload deferred while the tab/mode is hidden; replayed when shown again.
  const pendingReloadRef = useRef(true);
  // True while we still owe a one-time camera framing after a fresh load / layout.
  const framePendingRef = useRef(true);
  // Debounce handle for persisting the flat layout, and a flag marking that the
  // current load had no saved coordinates — so we persist the initial auto-layout
  // exactly once when the engine first settles (force + flat only).
  const saveTimerRef = useRef<number | null>(null);
  const persistOnSettleRef = useRef(false);
  // Whether the current node positions were pinned by the deterministic zoned
  // engine. Lets the live-sim path know it must release those pins.
  const deterministicPinnedRef = useRef(false);
  // Radial layout bookkeeping: whether the soft radial force is installed, and the
  // node currently anchored at the centre (so we can release it when leaving).
  const radialInstalledRef = useRef(false);
  const radialSeedRef = useRef<GraphNode | null>(null);
  // Graph id of the currently selected (centre) node — used by the link accessors
  // to light up its wiring and stream particles.
  const selectedGraphIdRef = useRef<string | null>(null);

  // The declarative view state (seed, depth, facets, layout, …). Seed tracks the
  // app selection by default (followSelection); lenses may pin or clear it.
  const [viewState, setViewState] = useState<GraphViewState>(() => ({
    ...defaultGraphViewState(),
    seed: selectedID || undefined,
  }));
  const followSelectionRef = useRef(true);

  // Link types drive both the query (which edges to fetch) and client visibility.
  // Soft/metadata off by default — they mesh everything together.
  const [linkTypes, setLinkTypes] = useState<Record<LinkType, boolean>>({hard: true, soft: false, metadata: false});
  const [nodeDistance, setNodeDistance] = useState(1.2);
  const appliedDistanceRef = useRef(nodeDistance);
  // Zoom slider position (0 = zoomed out / far, 100 = zoomed in / near). Kept in
  // sync with the camera dolly so scroll-wheel and slider agree.
  const [zoomPct, setZoomPct] = useState(45);
  const [autoRotate, setAutoRotate] = useState(false);
  const [focusMatches, setFocusMatches] = useState(true);
  const [status, setStatus] = useState<GraphStatus>('idle');
  const [message, setMessage] = useState('Open a workspace to inspect the graph.');
  const [data, setData] = useState<GraphData>({nodes: [], links: []});
  const [hulls, setHulls] = useState<Hull[]>([]);
  const [hiddenCount, setHiddenCount] = useState(0);
  // Number of groups when the view is aggregated into super-nodes (0 = not
  // aggregated). Drives the aggregate badge + click-to-drill-in behavior.
  const [aggregatedGroups, setAggregatedGroups] = useState(0);
  const [dims, setDims] = useState({width: 300, height: 300});

  const searchMatchSet = useMemo(() => new Set(searchMatchIds), [searchMatchIds]);

  // Note metadata keyed by id, for grouping + facet matching.
  const notesById = useMemo(() => {
    const map = new Map<string, models.NoteSummaryDTO>();
    for (const note of notes) {
      map.set(note.id, note);
    }
    return map;
  }, [notes]);

  // Refs mirror control/view state so the ForceGraph accessors (called per
  // node/link every frame) read current values without rebuilding the graph.
  const linkTypesRef = useRef(linkTypes);
  // Node radius multiplier, fixed at 1× (the size slider was removed as unhelpful).
  // Kept as a ref so the render accessors that scale by it stay unchanged.
  const nodeSizeRef = useRef(1);
  const themeRef = useRef(theme);
  const activeRef = useRef(active);
  const searchMatchRef = useRef(searchMatchSet);
  const searchActiveRef = useRef(searchActive);
  const focusMatchesRef = useRef(focusMatches);
  const selectedIDRef = useRef(selectedID);
  const flatRef = useRef(flat);
  const viewStateRef = useRef(viewState);
  const notesByIdRef = useRef(notesById);
  const depthRef = useRef(depth);
  linkTypesRef.current = linkTypes;
  themeRef.current = theme;
  activeRef.current = active;
  searchMatchRef.current = searchMatchSet;
  searchActiveRef.current = searchActive;
  focusMatchesRef.current = focusMatches;
  selectedIDRef.current = selectedID;
  flatRef.current = flat;
  viewStateRef.current = viewState;
  notesByIdRef.current = notesById;
  depthRef.current = depth;

  const linkVisible = useCallback((type: LinkType): boolean => linkTypesRef.current[type], []);

  // Whether a node passes the active facet filters. Note nodes are matched against
  // their metadata; hub nodes are always contextual (never dimmed by facets).
  const passesFacets = useCallback((node: FGNode): boolean => {
    const facets = viewStateRef.current.facets;
    if (facets.types.length === 0 && facets.tags.length === 0 && facets.folders.length === 0 && !facets.favorites) {
      return true;
    }
    if (HUB_NODE_KINDS.has(node.kind)) {
      return true;
    }
    const note = node.noteId ? notesByIdRef.current.get(node.noteId) : notesByIdRef.current.get(node.id);
    if (!note) {
      return false;
    }
    if (facets.types.length && !facets.types.includes(note.type)) {
      return false;
    }
    if (facets.tags.length && !(note.tags || []).some((tag) => facets.tags.includes(tag))) {
      return false;
    }
    if (facets.folders.length && !facets.folders.some((folder) => folderOf(note.path) === folder)) {
      return false;
    }
    if (facets.favorites && !note.favorite) {
      return false;
    }
    return true;
  }, []);

  // "Focus + context": a node is dimmed when it fails the active facet filter, or
  // (during search) is not a search match. The selected node is never dimmed.
  const nodeDimmed = useCallback((node: FGNode): boolean => {
    if (node.selected) {
      return false;
    }
    if (!passesFacets(node)) {
      return true;
    }
    if (searchActiveRef.current && focusMatchesRef.current) {
      return !node.noteId || !searchMatchRef.current.has(node.noteId);
    }
    return false;
  }, [passesFacets]);

  // Dolly the camera to a given distance from the current orbit target, keeping
  // the view direction (so the top-down flat view stays top-down). Drives the
  // zoom slider.
  const applyZoom = useCallback((distance: number) => {
    const fg = fgRef.current;
    if (!fg) {
      return;
    }
    const cam = fg.camera();
    const target = (fg.controls() as {target?: {x: number; y: number; z: number}} | undefined)?.target ?? {x: 0, y: 0, z: 0};
    const dx = cam.position.x - target.x;
    const dy = cam.position.y - target.y;
    const dz = cam.position.z - target.z;
    const current = Math.hypot(dx, dy, dz) || 1;
    const k = distance / current;
    fg.cameraPosition({x: target.x + dx * k, y: target.y + dy * k, z: target.z + dz * k}, target, 0);
  }, []);

  // Keep the zoom slider in step with the camera when the user scroll-zooms or the
  // camera flies (throttled to one read per frame).
  useEffect(() => {
    const controls = fgRef.current?.controls() as
      | {addEventListener?: (t: string, fn: () => void) => void; removeEventListener?: (t: string, fn: () => void) => void; target?: {x: number; y: number; z: number}}
      | undefined;
    if (!controls?.addEventListener) {
      return;
    }
    let raf = 0;
    const onChange = () => {
      if (raf) {
        return;
      }
      raf = requestAnimationFrame(() => {
        raf = 0;
        const fg = fgRef.current;
        if (!fg) {
          return;
        }
        const cam = fg.camera();
        const target = controls.target ?? {x: 0, y: 0, z: 0};
        const d = Math.hypot(cam.position.x - target.x, cam.position.y - target.y, cam.position.z - target.z);
        setZoomPct(zoomDistanceToPct(d));
      });
    };
    controls.addEventListener('change', onChange);
    return () => {
      controls.removeEventListener?.('change', onChange);
      if (raf) {
        cancelAnimationFrame(raf);
      }
    };
  }, [status]);

  // Fly the camera to look at a node from a fixed stand-off distance.
  const flyTo = useCallback((node: FGNode, ms: number) => {
    const fg = fgRef.current;
    if (!fg || node.x == null || node.y == null || node.z == null) {
      return;
    }
    if (flatRef.current) {
      fg.cameraPosition({x: node.x, y: node.y, z: CAMERA_DISTANCE}, {x: node.x, y: node.y, z: 0}, ms);
      return;
    }
    const dist = Math.hypot(node.x, node.y, node.z) || 1;
    const ratio = 1 + CAMERA_DISTANCE / dist;
    fg.cameraPosition({x: node.x * ratio, y: node.y * ratio, z: node.z * ratio}, node as {x: number; y: number; z: number}, ms);
  }, []);

  const load = useCallback(async () => {
    if (!workspaceOpen) {
      setStatus('idle');
      setMessage('Open a workspace to inspect the graph.');
      setData({nodes: [], links: []});
      setHulls([]);
      setHiddenCount(0);
      setAggregatedGroups(0);
      onStats({notes: 0, links: 0});
      return;
    }
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    setStatus('loading');
    try {
      const vs = viewStateRef.current;
      const query = toGraphQueryDTO({
        ...vs,
        depth: depthRef.current,
        includeSoftLinks: linkTypesRef.current.soft,
        includeMetadataLinks: linkTypesRef.current.metadata,
      });
      // The persisted {x,y} layout is only meaningful for the flat force view; the
      // deterministic engines compute positions fresh, so skip loading it there.
      const useSaved = flat && vs.layout === 'force';
      const [dto, saved] = await Promise.all([
        GraphQuery(query),
        useSaved ? LoadGraphLayout().catch(() => models.LayoutSnapshotDTO.createFrom({coordinates: {}})) : Promise.resolve(null),
      ]);
      if (requestRef.current !== requestID) {
        return;
      }
      const builtFull = buildData(dto, selectedIDRef.current, flat);
      // A large, unfocused graph collapses into a group-level overview; otherwise
      // it's bounded by the render cap. Both keep the sim responsive at scale.
      let built: GraphData;
      let hidden = 0;
      if (!vs.seed && builtFull.nodes.length > AGGREGATE_THRESHOLD) {
        built = aggregateGraph(builtFull, vs.groupBy, notesByIdRef.current);
        setAggregatedGroups(built.nodes.length);
      } else {
        const capped = capGraph(builtFull, NODE_RENDER_CAP, selectedIDRef.current);
        built = capped.data;
        hidden = capped.hidden;
        setAggregatedGroups(0);
      }
      setHiddenCount(hidden);
      // Restore the saved {x,y} layout unless it has degenerated to a near-line
      // (the "diagonal stretch" bug): a stretched layout, restored and pinned,
      // can't be fixed by the sim. Skipping it lets the force layout respread and
      // re-persist a healthy layout, so it self-heals instead of needing a manual
      // layout round-trip.
      const hadSaved =
        useSaved && saved && !isDegenerateLayout(saved.coordinates)
          ? applySavedLayout(built.nodes, saved.coordinates)
          : false;
      persistOnSettleRef.current = useSaved && !hadSaved;
      selectedGraphIdRef.current = built.nodes.find((n) => n.selected)?.id ?? null;
      setData(built);
      framePendingRef.current = true;
      setStatus('ready');
      reportStats(built, linkTypesRef.current, onStats);
    } catch (err) {
      if (requestRef.current !== requestID) {
        return;
      }
      const text = errorMessage(err);
      setStatus('error');
      setMessage(text);
      onError(text);
    }
  }, [workspaceOpen, flat, onError, onStats]);

  // A signature over every query-affecting input. Changing the layout, grouping or
  // hide toggle does NOT change this, so those stay client-side (no refetch).
  const queryKey = useMemo(
    () =>
      JSON.stringify({
        ws: workspaceOpen,
        seed: viewState.seed ?? '',
        depth,
        types: viewState.facets.types,
        tags: viewState.facets.tags,
        folders: viewState.facets.folders,
        favorites: viewState.facets.favorites,
        unresolved: viewState.includeUnresolved,
        soft: linkTypes.soft,
        meta: linkTypes.metadata,
      }),
    [workspaceOpen, viewState.seed, depth, viewState.facets, viewState.includeUnresolved, linkTypes.soft, linkTypes.metadata],
  );

  // Seed tracks the app selection unless a lens has pinned/cleared it.
  useEffect(() => {
    if (!followSelectionRef.current) {
      return;
    }
    const next = selectedID || undefined;
    setViewState((state) => (state.seed === next ? state : {...state, seed: next}));
  }, [selectedID]);

  // Facets are owned by App (shared with the note-list filter). Mirror the prop
  // into viewState so the existing query/match/dim reads stay unchanged; the
  // panel that edits them lives in the right rail, and drill-in calls back up.
  useEffect(() => {
    setViewState((state) => (state.facets === facets ? state : {...state, facets}));
  }, [facets]);

  // Reload when the query changes — deferred while hidden, replayed on show.
  useEffect(() => {
    pendingReloadRef.current = true;
    if (activeRef.current) {
      pendingReloadRef.current = false;
      void load();
    }
  }, [queryKey, refreshKey, load]);

  // React to becoming visible: resume the render loop, resize, run any deferred
  // reload. Pause the loop when hidden to save CPU/GPU.
  useEffect(() => {
    const fg = fgRef.current;
    if (!active) {
      fg?.pauseAnimation();
      return;
    }
    fg?.resumeAnimation();
    measure();
    if (pendingReloadRef.current) {
      pendingReloadRef.current = false;
      void load();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, load]);

  // Keep visible-link stats in sync when link-type toggles change.
  useEffect(() => {
    reportStats(data, linkTypes, onStats);
  }, [linkTypes, data, onStats]);

  // Camera controls: flat mode disables rotation (pan + zoom only) and remaps LEFT
  // to pan; orbit mode restores LEFT=rotate and the built-in autoRotate.
  useEffect(() => {
    const controls = fgRef.current?.controls() as
      | {
          autoRotate?: boolean;
          autoRotateSpeed?: number;
          enableRotate?: boolean;
          screenSpacePanning?: boolean;
          minDistance?: number;
          maxDistance?: number;
          zoomSpeed?: number;
          mouseButtons?: {LEFT?: number; MIDDLE?: number; RIGHT?: number};
        }
      | undefined;
    if (controls) {
      controls.enableRotate = !flat;
      controls.autoRotate = flat ? false : autoRotate;
      controls.autoRotateSpeed = 0.9;
      controls.screenSpacePanning = flat;
      // Clamp the dolly range so a node can't fill the whole viewport (min) and the
      // graph can't be flung out of sight (max), and speed up the scroll wheel — a
      // single node was previously easy to zoom past. A dedicated slider (Task #6)
      // will make the range adjustable.
      controls.minDistance = ZOOM_MIN_DISTANCE;
      controls.maxDistance = ZOOM_MAX_DISTANCE;
      controls.zoomSpeed = 1.8;
      if (controls.mouseButtons) {
        controls.mouseButtons.LEFT = flat ? MOUSE.PAN : MOUSE.ROTATE;
      }
    }
  }, [autoRotate, status, flat]);

  // Nudge the renderer to re-evaluate cached accessors when a purely client-side
  // input changes (link toggles, node size, search spotlight, facet emphasis).
  useEffect(() => {
    if (status === 'ready') {
      fgRef.current?.refresh();
    }
  }, [linkTypes, searchMatchSet, searchActive, focusMatches, viewState.facets, viewState.hideNonMatches, status]);

  // Layout strategy.
  //  - force : the live d3 simulation, unconstrained (global structure).
  //  - radial: the live simulation PLUS a soft forceRadial that pulls each node
  //            toward a ring at radius ∝ its hop-distance from the focus, with the
  //            focus anchored at the origin. Organic and data-dependent — not the
  //            rigid, always-identical concentric rings of a deterministic engine.
  //  - zoned : a deterministic engine whose positions are pinned onto the nodes,
  //            with a rounded hull + label drawn per group.
  // Recomputed when the layout, grouping, focus, spacing or graph data changes.
  useEffect(() => {
    const fg = fgRef.current;
    if (!fg || status !== 'ready') {
      return;
    }
    const kind = viewState.layout;

    // Remove the radial force + release the node we anchored at the centre.
    const teardownRadial = () => {
      if (radialInstalledRef.current) {
        fg.d3Force('radial', null);
        radialInstalledRef.current = false;
      }
      if (radialSeedRef.current) {
        radialSeedRef.current.fx = undefined;
        radialSeedRef.current.fy = undefined;
        radialSeedRef.current.fz = flat ? 0 : undefined;
        radialSeedRef.current = null;
      }
    };
    const setCentering = (on: boolean) => {
      fg.d3Force('x', on ? forceX(0).strength(CENTER_STRENGTH) : null);
      fg.d3Force('y', on ? forceY(0).strength(CENTER_STRENGTH) : null);
    };

    // Applies the whole layout. react-force-graph rebuilds its d3 simulation
    // whenever graphData changes, discarding any custom forces we installed on the
    // previous sim — so on a fresh load our radial/centering/charge forces would be
    // silently dropped (the "looks wrong until you swap to Zoned and back" bug).
    // We therefore run this once synchronously AND again on the next animation
    // frame (post-commit), by which point the library has re-created its sim, so
    // the forces stick.
    const applyLayout = () => {
      if (kind === 'zoned') {
        teardownRadial();
        setCentering(false);
        const engine = layoutEngineFor('zoned');
        const layoutNodes: LayoutNode[] = data.nodes.map((node) => ({
          id: node.id,
          degree: node.degree,
          selected: node.selected,
          hops: node.hops,
          group: groupKeyFor(node, viewState.groupBy, notesById),
        }));
        const layoutEdges: LayoutEdge[] = data.links.map((link) => ({source: endId(link.source), target: endId(link.target)}));
        const result = engine(layoutNodes, layoutEdges, {spacing: nodeDistance});
        for (const node of data.nodes) {
          const point = result.positions[node.id];
          if (point) {
            node.x = node.fx = point.x;
            node.y = node.fy = point.y;
            node.z = node.fz = 0;
          }
        }
        deterministicPinnedRef.current = true;
        appliedDistanceRef.current = nodeDistance;
        setHulls(flat ? result.hulls ?? [] : []);
        fg.d3ReheatSimulation();
        return;
      }

      // force / radial share the live simulation. Release any deterministic pins
      // left by a prior zoned layout so the sim can move nodes. A fresh force load
      // keeps its saved-layout pins (applied in load()), so only clear when leaving
      // a pinned layout.
      if (deterministicPinnedRef.current) {
        for (const node of data.nodes) {
          node.fx = undefined;
          node.fy = undefined;
          node.fz = flat ? 0 : undefined;
          if (!flat) {
            node.z = undefined;
          }
        }
        deterministicPinnedRef.current = false;
      }
      setHulls([]);

      if (kind === 'radial') {
        // Unpin the previous centre, anchor the current focus at the origin, and
        // install the soft radial force keyed to each node's hop-distance.
        if (radialSeedRef.current) {
          radialSeedRef.current.fx = undefined;
          radialSeedRef.current.fy = undefined;
        }
        const seedId = viewState.seed;
        const seed =
          data.nodes.find((n) => n.selected) ??
          (seedId ? data.nodes.find((n) => n.noteId === seedId || n.id === seedId) : undefined);
        const gap = RADIAL_RING_GAP * nodeDistance;
        let maxHop = 0;
        for (const node of data.nodes) {
          if (node.hops != null && node.hops > maxHop) {
            maxHop = node.hops;
          }
        }
        const outerRadius = (maxHop + 1) * gap;
        const radiusById = new Map<string, number>();
        for (const node of data.nodes) {
          radiusById.set(node.id, node.hops == null ? outerRadius : node.hops * gap);
        }
        if (seed) {
          seed.fx = 0;
          seed.fy = 0;
          seed.fz = 0;
          radialSeedRef.current = seed;
        } else {
          radialSeedRef.current = null;
        }
        fg.d3Force(
          'radial',
          forceRadial((node: {id: string}) => radiusById.get(node.id) ?? outerRadius, 0, 0, 0).strength(RADIAL_STRENGTH),
        );
        radialInstalledRef.current = true;
        setCentering(false); // forceRadial already anchors the layout at the origin
      } else {
        teardownRadial();
        setCentering(true);
      }

      fg.d3ReheatSimulation();
    };

    applyLayout();
    const raf = requestAnimationFrame(applyLayout);
    return () => cancelAnimationFrame(raf);
  }, [viewState.layout, viewState.groupBy, viewState.seed, data, nodeDistance, status, flat, notesById]);

  // Node spacing (live-sim layouts): scale the charge/link forces by the Distance
  // slider and reheat so the layout respreads. The zoned layout is deterministic
  // (positions pinned), so it's driven by its own effect and skipped here.
  useEffect(() => {
    const fg = fgRef.current;
    if (!fg || status !== 'ready' || viewState.layout === 'zoned') {
      return;
    }
    const distanceChanged = appliedDistanceRef.current !== nodeDistance;
    appliedDistanceRef.current = nodeDistance;
    // Charge/link strengths live on the sim's forces, which the library rebuilds on
    // each graphData change — so re-assert them once synchronously and again on the
    // next frame (after the library has re-created the sim), matching applyLayout.
    const radialCut = viewState.layout === 'radial' ? RADIAL_CHARGE_SCALE : 1;
    const applyStrengths = () => {
      const charge = fg.d3Force('charge') as
        | {strength?: (v: number | ((node: FGNode) => number)) => unknown}
        | undefined;
      // Per-node repulsion: the selected hub is damped so it doesn't fling its
      // neighbours away, and radial damps every node so they hold their rings.
      charge?.strength?.((node: FGNode) =>
        BASE_CHARGE * nodeDistance * radialCut * (node.selected ? SELECTED_CHARGE_SCALE : 1),
      );
      const link = fg.d3Force('link') as {distance?: (v: number) => unknown} | undefined;
      link?.distance?.(BASE_LINK_DISTANCE * nodeDistance);
    };
    // Flat force mode pins nodes to their saved/dragged {x,y}; release those pins
    // on a real slider change so the graph respreads, then persist once it settles.
    // (Radial anchors only the focus, so it has no saved-layout pins to release.)
    if (distanceChanged && flatRef.current && viewState.layout === 'force') {
      for (const node of data.nodes) {
        node.fx = undefined;
        node.fy = undefined;
        node.fz = 0;
      }
      persistOnSettleRef.current = true;
      framePendingRef.current = false;
    }
    applyStrengths();
    fg.d3ReheatSimulation();
    const raf = requestAnimationFrame(() => {
      applyStrengths();
      fg.d3ReheatSimulation();
    });
    return () => cancelAnimationFrame(raf);
  }, [nodeDistance, status, data, viewState.layout]);

  // Draw zoned-layout hulls as translucent shapes behind the nodes, each with a
  // bold group label at its centroid so the colours are identifiable (flat mode).
  useEffect(() => {
    const scene = fgRef.current?.scene();
    if (!scene || !flat || hulls.length === 0) {
      return;
    }
    const added: Object3D[] = [];
    for (const hull of hulls) {
      if (hull.points.length < 3) {
        continue;
      }
      const color = groupColor(hull.group, theme);
      const shape = new Shape();
      shape.moveTo(hull.points[0].x, hull.points[0].y);
      for (let i = 1; i < hull.points.length; i += 1) {
        shape.lineTo(hull.points[i].x, hull.points[i].y);
      }
      shape.closePath();
      const geometry = new ShapeGeometry(shape);
      const material = new MeshBasicMaterial({
        color: new Color(color),
        transparent: true,
        opacity: theme === 'dark' ? 0.14 : 0.11,
        side: DoubleSide,
        depthWrite: false,
      });
      const mesh = new Mesh(geometry, material);
      mesh.position.z = -2; // sit behind the z=0 node plane
      mesh.renderOrder = -1;
      scene.add(mesh);
      added.push(mesh);

      // Group label, pushed to the OUTER edge of the zone (centroid + outward
      // normal × hull-radius) so nodes don't overwrite it, and drawn in front of
      // everything (positive z, high renderOrder, depth test off) with a
      // contrasting outline so it stays readable over both nodes and hulls.
      let cx = 0;
      let cy = 0;
      for (const point of hull.points) {
        cx += point.x;
        cy += point.y;
      }
      cx /= hull.points.length;
      cy /= hull.points.length;
      let radius = 0;
      for (const point of hull.points) {
        radius = Math.max(radius, Math.hypot(point.x - cx, point.y - cy));
      }
      // Outward direction from the graph origin; fall back to straight up when the
      // centroid sits essentially at the centre (e.g. a single zone).
      const centroidDist = Math.hypot(cx, cy);
      const ux = centroidDist > 1 ? cx / centroidDist : 0;
      const uy = centroidDist > 1 ? cy / centroidDist : 1;
      const margin = 14;
      const label = new SpriteText(hull.group);
      label.color = color;
      // Screen-fixed (sizeAttenuation off) so a zone name is readable at any zoom.
      label.textHeight = ZONE_LABEL_HEIGHT;
      label.fontFace = 'Segoe UI, Arial, sans-serif';
      label.fontWeight = '700';
      label.strokeWidth = 0.8;
      label.strokeColor = theme === 'dark' ? '#0b0b10' : '#ffffff';
      const labelMat = label.material as SpriteMaterial;
      labelMat.sizeAttenuation = false;
      labelMat.needsUpdate = true;
      labelMat.depthWrite = false;
      labelMat.depthTest = false;
      label.renderOrder = 10;
      label.position.set(cx + ux * (radius + margin), cy + uy * (radius + margin), 5);
      scene.add(label);
      added.push(label);
    }
    return () => {
      for (const obj of added) {
        scene.remove(obj);
        const mesh = obj as Mesh;
        mesh.geometry?.dispose?.();
        const mat = mesh.material;
        if (mat && !Array.isArray(mat)) {
          (mat as MeshBasicMaterial).dispose();
        }
      }
    };
  }, [hulls, flat, theme, status]);

  // Selection highlight + gentle center, independent of any query reload. Clicking
  // a node (or selecting a note elsewhere) marks it selected, lights up its wiring,
  // grows it, and eases the camera onto it — even when the seed isn't following the
  // selection (e.g. under a lens), so a click always gives visible feedback. When
  // the seed IS following, the subsequent reload rebuilds the neighborhood and the
  // engine-stop handler frames the settled result.
  const centeredSelectionRef = useRef('');
  useEffect(() => {
    if (status !== 'ready') {
      return;
    }
    let target: GraphNode | undefined;
    for (const node of data.nodes) {
      const isSelected = Boolean(selectedID) && (node.noteId ? node.noteId === selectedID : node.id === selectedID);
      node.selected = isSelected;
      if (isSelected) {
        target = node;
      }
    }
    selectedGraphIdRef.current = target?.id ?? null;
    fgRef.current?.refresh();
    if (!selectedID) {
      centeredSelectionRef.current = '';
      return;
    }
    // Fly only when the selection actually changed, not on every reload re-settle.
    if (selectedID !== centeredSelectionRef.current) {
      centeredSelectionRef.current = selectedID;
      if (target) {
        flyTo(target as FGNode, 300);
      }
    }
  }, [selectedID, data, status, flyTo]);

  // Frame the view ONCE per load — never on a slider/layout tweak (those don't
  // change `data`, so this effect doesn't re-run for them). warmupTicks pre-settles
  // the layout before the first paint, so framing on the next animation frames
  // catches an already-spread graph and the camera settles immediately rather than
  // lurching seconds later. A selected node is framed by the selection effect
  // above; here we only fit the whole graph when nothing is selected.
  useEffect(() => {
    if (status !== 'ready' || !framePendingRef.current) {
      return;
    }
    const hasSelection = Boolean(selectedIDRef.current)
      && data.nodes.some((n) => (n.noteId ? n.noteId === selectedIDRef.current : n.id === selectedIDRef.current));
    if (hasSelection) {
      framePendingRef.current = false;
      return;
    }
    let cancelled = false;
    let handle = requestAnimationFrame(() => {
      handle = requestAnimationFrame(() => {
        if (cancelled || !framePendingRef.current) {
          return;
        }
        framePendingRef.current = false;
        fgRef.current?.zoomToFit(400, 40);
      });
    });
    return () => {
      cancelled = true;
      cancelAnimationFrame(handle);
    };
  }, [status, data]);

  // Measure the stage so the canvas fills it.
  const measure = useCallback(() => {
    const el = stageRef.current;
    if (!el) {
      return;
    }
    const {clientWidth, clientHeight} = el;
    if (clientWidth > 0 && clientHeight > 0) {
      setDims((prev) => (prev.width === clientWidth && prev.height === clientHeight ? prev : {width: clientWidth, height: clientHeight}));
    }
  }, []);

  useEffect(() => {
    const el = stageRef.current;
    if (!el || typeof ResizeObserver === 'undefined') {
      return;
    }
    const ro = new ResizeObserver(() => measure());
    ro.observe(el);
    measure();
    return () => ro.disconnect();
  }, [measure]);

  useEffect(() => () => {
    if (clickTimerRef.current !== null) {
      window.clearTimeout(clickTimerRef.current);
      clickTimerRef.current = null;
    }
    if (saveTimerRef.current !== null) {
      window.clearTimeout(saveTimerRef.current);
      saveTimerRef.current = null;
    }
  }, []);

  // Persist the current flat layout ({x,y} per node), debounced. Force + flat only.
  const saveFlatLayout = useCallback((delay = 500) => {
    if (saveTimerRef.current !== null) {
      window.clearTimeout(saveTimerRef.current);
    }
    saveTimerRef.current = window.setTimeout(() => {
      saveTimerRef.current = null;
      const coordinates: Record<string, {x: number; y: number}> = {};
      for (const node of data.nodes) {
        if (Number.isFinite(node.x) && Number.isFinite(node.y)) {
          coordinates[node.id] = {x: node.x as number, y: node.y as number};
        }
      }
      void SaveGraphLayout(models.LayoutSnapshotDTO.createFrom({coordinates, updatedAt: new Date().toISOString()}))
        .catch((err) => onError(errorMessage(err)));
    }, delay);
  }, [data, onError]);

  // Pin a dragged node where the user dropped it, then persist (force + flat only).
  const handleNodeDragEnd = useCallback((node: FGNode) => {
    node.fx = node.x;
    node.fy = node.y;
    node.fz = 0;
    saveFlatLayout();
  }, [saveFlatLayout]);

  // Drill into an aggregate super-node: add its group to the matching facet so the
  // query narrows to that group's notes (which then render individually).
  const drillIntoAggregate = useCallback((id: string) => {
    const match = /^agg:(types|tags|folders):(.*)$/.exec(id);
    if (!match) {
      return;
    }
    const axis = match[1] as 'types' | 'tags' | 'folders';
    const key = match[2];
    followSelectionRef.current = false;
    // Facets live in App; push the added group up so the rail chips and the
    // note-list filter stay in sync (the sync effect mirrors it back into viewState).
    const current = viewStateRef.current.facets[axis];
    if (current.includes(key)) {
      return;
    }
    onFacetsChange({...viewStateRef.current.facets, [axis]: [...current, key]});
  }, [onFacetsChange]);

  const selectNode = useCallback((node: FGNode) => {
    if (node.kind === 'aggregate') {
      drillIntoAggregate(node.id);
      return;
    }
    if (node.noteId) {
      onSelectNote(node.noteId);
    }
    flyTo(node, 300);
  }, [onSelectNote, flyTo, drillIntoAggregate]);

  const openNode = useCallback((node: FGNode) => {
    if (node.noteId) {
      onOpenNote(node.noteId);
    }
  }, [onOpenNote]);

  // Single click selects (debounced), double click opens.
  const handleNodeClick = useCallback((node: FGNode) => {
    if (clickTimerRef.current !== null) {
      window.clearTimeout(clickTimerRef.current);
      clickTimerRef.current = null;
      openNode(node);
      return;
    }
    clickTimerRef.current = window.setTimeout(() => {
      clickTimerRef.current = null;
      selectNode(node);
    }, CLICK_DELAY);
  }, [openNode, selectNode]);

  // When the layout settles, persist the settled auto-layout (force + flat with no
  // pre-saved layout). Camera framing is NOT done here — engine-stop fires seconds
  // after the graph is already on screen, and that late jump was the distracting
  // "delayed camera move". Framing now happens once right after load (see below).
  const handleEngineStop = useCallback(() => {
    if (persistOnSettleRef.current) {
      persistOnSettleRef.current = false;
      saveFlatLayout(0);
    }
  }, [saveFlatLayout]);

  const nodeColorAccessor = useCallback((node: FGNode): string => {
    const dark = themeRef.current === 'dark';
    if (node.selected) {
      return nodeColor(node.kind, true);
    }
    if (nodeDimmed(node)) {
      return dark ? '#3a4247' : '#d8d2c4';
    }
    if (searchActiveRef.current && focusMatchesRef.current && node.noteId && searchMatchRef.current.has(node.noteId)) {
      return '#f0c651'; // spotlight match
    }
    if (HUB_NODE_KINDS.has(node.kind) || node.kind === 'unresolved') {
      return nodeColor(node.kind, false);
    }
    if (node.hops != null && node.hops > 0) {
      return depthShade(node.hops, themeRef.current);
    }
    return noteShade(node.degree, themeRef.current);
  }, [nodeDimmed]);

  const nodeVisible = useCallback((node: FGNode): boolean => {
    // Hub nodes (tag/type/heading) only appear when metadata links are shown.
    if (HUB_NODE_KINDS.has(node.kind)) {
      return linkTypesRef.current.metadata;
    }
    // Hide-non-matches: drop nodes that fail the facet / search filter.
    if (viewStateRef.current.hideNonMatches && !node.selected && nodeDimmed(node)) {
      return false;
    }
    return true;
  }, [nodeDimmed]);

  // Selected nodes pop (even when no reload rebuilt them at full size); other
  // non-matching nodes shrink (as well as dim) so matches read as prominent.
  const nodeVal = useCallback((node: FGNode): number => {
    if (node.selected) {
      return Math.max(node.val, SELECTED_VAL) * nodeSizeRef.current;
    }
    const base = node.val * nodeSizeRef.current;
    return nodeDimmed(node) ? base * 0.5 : base;
  }, [nodeDimmed]);

  const linkVisibleAccessor = useCallback((link: FGLink): boolean => linkVisible(link.linkType), [linkVisible]);

  const linkTouchesSelected = useCallback((link: FGLink): boolean => {
    const sel = selectedGraphIdRef.current;
    if (!sel) {
      return false;
    }
    const s = link.source as FGNode | string | undefined;
    const t = link.target as FGNode | string | undefined;
    const sID = typeof s === 'object' && s ? s.id : s;
    const tID = typeof t === 'object' && t ? t.id : t;
    return sID === sel || tID === sel;
  }, []);

  const linkColorAccessor = useCallback((link: FGLink): string => {
    if (linkTouchesSelected(link)) {
      const dark = themeRef.current === 'dark';
      switch (link.linkType) {
        case 'soft':
          return dark ? '#f4c66a' : '#c9861f';
        case 'metadata':
          return dark ? '#e0a3f0' : '#a63fbc';
        default:
          return dark ? '#9fb4ff' : '#3a52d6';
      }
    }
    if (searchActiveRef.current && focusMatchesRef.current) {
      const s = link.source as FGNode;
      const t = link.target as FGNode;
      const sMatch = s?.noteId ? searchMatchRef.current.has(s.noteId) : false;
      const tMatch = t?.noteId ? searchMatchRef.current.has(t.noteId) : false;
      if (!sMatch && !tMatch) {
        return themeRef.current === 'dark' ? '#2c2f33' : '#e7e2d7';
      }
    }
    return edgeColor(link.linkType, themeRef.current);
  }, [linkTouchesSelected]);

  const linkWidthAccessor = useCallback((link: FGLink): number => (linkTouchesSelected(link) ? 1.6 : 0.6), [linkTouchesSelected]);

  const linkParticles = useCallback(
    (link: FGLink): number => (linkTouchesSelected(link) && linkVisible(link.linkType) ? SELECTED_PARTICLES : 0),
    [linkTouchesSelected, linkVisible],
  );

  const linkParticleColor = useCallback((link: FGLink): string => linkColorAccessor(link), [linkColorAccessor]);

  const nodeThreeObject = useCallback((node: FGNode): Object3D => {
    const isHub = HUB_NODE_KINDS.has(node.kind);
    const labelAll = data.nodes.length <= LABEL_ALL_MAX_NODES;
    // On larger graphs only the selected node, hubs, and well-connected notes get
    // a label, so the scene stays legible instead of a wall of overlapping text.
    const labelWorthy = node.selected || isHub || labelAll || node.degree >= DEGREE_LABEL_MIN;
    if (!labelWorthy) {
      return new Object3D(); // sphere only
    }
    const sprite = new SpriteText(node.label);
    sprite.color = labelColor(node.kind, Boolean(node.selected), themeRef.current);
    sprite.fontFace = 'Segoe UI, Arial, sans-serif';
    sprite.fontWeight = node.selected ? '700' : '500';
    const radius = Math.cbrt(Math.max(1, node.val * nodeSizeRef.current)) * 4;
    if (node.selected) {
      // The current note's label stays a fixed on-screen size at any zoom (like the
      // hover tooltip) and draws in front of everything.
      const mat = sprite.material as SpriteMaterial;
      mat.sizeAttenuation = false;
      mat.needsUpdate = true;
      mat.depthWrite = false;
      mat.depthTest = false;
      sprite.renderOrder = 11;
      sprite.textHeight = SELECTED_LABEL_HEIGHT;
      sprite.position.set(0, radius + 6, 0);
    } else {
      sprite.textHeight = isHub ? HUB_LABEL_HEIGHT : NODE_LABEL_HEIGHT;
      (sprite.material as {depthWrite?: boolean}).depthWrite = false;
      sprite.position.set(0, radius + sprite.textHeight * 0.9, 0);
    }
    return sprite;
  }, [data.nodes.length, theme]);

  const toggleLinkType = useCallback((type: LinkType) => {
    setLinkTypes((current) => ({...current, [type]: !current[type]}));
  }, []);

  const {matchCount, totalCount} = useMemo(() => {
    let total = 0;
    let match = 0;
    const facets = viewState.facets;
    const anyFacet = facets.types.length > 0 || facets.tags.length > 0 || facets.folders.length > 0 || facets.favorites;
    for (const node of data.nodes) {
      if (HUB_NODE_KINDS.has(node.kind) || node.kind === 'unresolved') {
        continue;
      }
      total += 1;
      if (!anyFacet) {
        match += 1;
        continue;
      }
      const note = node.noteId ? notesById.get(node.noteId) : notesById.get(node.id);
      if (facetMatchesNote(note, facets)) {
        match += 1;
      }
    }
    return {matchCount: match, totalCount: total};
  }, [data, viewState.facets, notesById]);

  const resetView = useCallback(() => {
    fgRef.current?.zoomToFit(500, 40);
  }, []);

  const showOverlay = status === 'idle' || status === 'error';

  return (
    <div className="gm-graph-layout">
      <div className="gm-graph-stage" ref={stageRef}>
        <div className="gm-graph-canvas gm-graph-canvas--live gm-graph-canvas--3d">
          <ForceGraph3D<GraphNode, GraphLink>
            ref={fgRef as never}
            width={dims.width}
            height={dims.height}
            graphData={data}
            backgroundColor={backgroundColor(theme)}
            showNavInfo={false}
            controlType="orbit"
            numDimensions={flat ? 2 : 3}
            nodeRelSize={4}
            nodeVal={nodeVal}
            nodeColor={nodeColorAccessor}
            nodeVisibility={nodeVisible}
            nodeLabel={(node: FGNode) => node.label}
            nodeOpacity={0.92}
            nodeResolution={12}
            nodeThreeObject={nodeThreeObject}
            nodeThreeObjectExtend
            linkColor={linkColorAccessor}
            linkVisibility={linkVisibleAccessor}
            linkWidth={linkWidthAccessor}
            linkOpacity={0.45}
            linkDirectionalParticles={linkParticles}
            linkDirectionalParticleWidth={1.6}
            linkDirectionalParticleSpeed={0.01}
            linkDirectionalParticleColor={linkParticleColor}
            enableNodeDrag
            onNodeClick={handleNodeClick}
            onNodeRightClick={openNode}
            onNodeDragEnd={flat && viewState.layout === 'force' ? handleNodeDragEnd : undefined}
            onEngineStop={handleEngineStop}
            warmupTicks={60}
            cooldownTicks={200}
          />
        </div>
        {showOverlay && <div className="gm-graph-empty">{message}</div>}
        {status === 'loading' && <div className="gm-graph-badge">Loading…</div>}
        {status === 'ready' && aggregatedGroups > 0 && (
          <div
            className="gm-graph-badge gm-graph-badge--aggregate"
            title="The graph is large, so notes are grouped. Click a group to expand it; switch the Zoned “group by” to regroup."
          >
            {aggregatedGroups} groups · click to expand
          </div>
        )}
        {status === 'ready' && hiddenCount > 0 && (
          <div
            className="gm-graph-badge gm-graph-badge--cap"
            style={{top: 'auto', bottom: 8, right: 8, left: 'auto'}}
            title={`Only the ${NODE_RENDER_CAP.toLocaleString()} most-connected nodes are drawn. Focus a note or add a filter to narrow the view.`}
          >
            +{hiddenCount.toLocaleString()} more not shown
          </div>
        )}
        {status === 'ready' && data.nodes.length > 0 && (
          <div
            className="gm-graph-legend"
            style={{
              position: 'absolute',
              left: 8,
              bottom: 8,
              display: 'flex',
              flexDirection: 'column',
              gap: 3,
              padding: '6px 9px',
              borderRadius: 6,
              fontSize: 11,
              lineHeight: 1.35,
              maxWidth: 190,
              pointerEvents: 'none',
              background: theme === 'dark' ? 'rgba(20,20,25,0.72)' : 'rgba(255,255,255,0.82)',
              color: theme === 'dark' ? '#c9c6d0' : '#4a463f',
            }}
          >
            {viewState.layout === 'zoned' && hulls.length > 0 ? (
              <>
                <span style={{fontWeight: 600, marginBottom: 2}}>Zones by {viewState.groupBy ?? 'folder'}</span>
                {hulls.slice(0, 8).map((hull) => (
                  <LegendDot key={hull.group} color={groupColor(hull.group, theme)} label={hull.group} />
                ))}
                {hulls.length > 8 && <span style={{opacity: 0.7}}>+{hulls.length - 8} more</span>}
              </>
            ) : (
              <>
                <LegendDot color={nodeColor('note', true)} label="Selected" />
                {selectedID ? (
                  <>
                    <LegendDot color={depthShade(1, theme)} label="Near the focus" />
                    <LegendDot color={depthShade(MAX_DEPTH_HOPS, theme)} label="Farther out" />
                  </>
                ) : (
                  <>
                    <LegendDot color={noteShade(24, theme)} label="Well-linked note" />
                    <LegendDot color={noteShade(0, theme)} label="Leaf note" />
                  </>
                )}
                {linkTypes.metadata && <LegendDot color={nodeColor('tag', false)} label="Tag / type hub" />}
                {linkTypes.hard && <LegendLine color={edgeColor('hard', theme)} label="Hard link" />}
                {linkTypes.soft && <LegendLine color={edgeColor('soft', theme)} label="Soft link" />}
                {linkTypes.metadata && <LegendLine color={edgeColor('metadata', theme)} label="Metadata link" />}
              </>
            )}
          </div>
        )}
        {status === 'ready' && data.nodes.length > 0 && (
          <div
            className="gm-graph-zoom"
            style={{
              position: 'absolute',
              left: '50%',
              bottom: 10,
              transform: 'translateX(-50%)',
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '5px 12px',
              borderRadius: 999,
              fontSize: 11,
              pointerEvents: 'auto',
              background: theme === 'dark' ? 'rgba(20,20,25,0.78)' : 'rgba(255,255,255,0.86)',
              color: theme === 'dark' ? '#c9c6d0' : '#4a463f',
              boxShadow: '0 1px 6px rgba(0,0,0,0.18)',
            }}
          >
            <span aria-hidden style={{opacity: 0.7}}>−</span>
            <input
              aria-label="Zoom"
              type="range"
              min={0}
              max={100}
              step={1}
              value={zoomPct}
              onChange={(event) => {
                const pct = Number(event.target.value);
                setZoomPct(pct);
                applyZoom(zoomPctToDistance(pct));
              }}
              style={{width: 160}}
            />
            <span aria-hidden style={{opacity: 0.7}}>+</span>
          </div>
        )}
      </div>
      <aside className="gm-graph-options" aria-label="Graph options">
        <h3 className="gm-graph-options-title">Graph options · {flat ? '2D' : '3D'}</h3>

        <GraphFilterPanel
          state={viewState}
          matchCount={matchCount}
          totalCount={totalCount}
          onChange={setViewState}
          allowZoned={flat}
        />

        <div className="gm-graph-opt">
          <label className="gm-graph-opt-label" htmlFor={`gm-depth-${flat ? '2d' : '3d'}`}>
            Depth <span className="gm-graph-opt-value">{depth}</span>
          </label>
          <input
            id={`gm-depth-${flat ? '2d' : '3d'}`}
            type="range"
            min={DEPTH_OPTIONS[0]}
            max={DEPTH_OPTIONS[DEPTH_OPTIONS.length - 1]}
            step={1}
            value={depth}
            onChange={(event) => onDepthChange(Number(event.target.value))}
          />
          <div className="gm-graph-ticks">
            {DEPTH_OPTIONS.map((value) => <span key={value}>{value}</span>)}
          </div>
          <p className="gm-graph-opt-hint">Nodes within this many hops of the focused note.</p>
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
          <label className="gm-graph-opt-label sub" htmlFor={`gm-node-distance-${flat ? '2d' : '3d'}`}>
            Distance <span className="gm-graph-opt-value">{nodeDistance.toFixed(1)}×</span>
          </label>
          <input
            id={`gm-node-distance-${flat ? '2d' : '3d'}`}
            type="range"
            min={0.5}
            max={2.5}
            step={0.1}
            value={nodeDistance}
            onChange={(event) => setNodeDistance(Number(event.target.value))}
          />
        </div>

        <div className="gm-graph-opt">
          <span className="gm-graph-opt-label">{flat ? 'Focus' : 'Motion & focus'}</span>
          {!flat && (
            <label className="gm-graph-check">
              <input type="checkbox" checked={autoRotate} onChange={() => setAutoRotate((on) => !on)} />
              Auto-rotate the scene
            </label>
          )}
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
        <p className="gm-graph-opt-hint">
          {flat
            ? 'Drag to pan, scroll to zoom. In Force layout, drag a node to move it (position is saved). Click to select, double-click or right-click to open.'
            : 'Drag to orbit, scroll to zoom. Drag a node to move it. Click to select, double-click or right-click to open.'}
        </p>
      </aside>
    </div>
  );
}

// The zoned-layout group key for a node, per the active grouping. Hub / unresolved
// nodes and notes without metadata collapse into "Other".
function groupKeyFor(
  node: GraphNode,
  groupBy: GraphViewState['groupBy'],
  notesById: Map<string, models.NoteSummaryDTO>,
): string {
  if (HUB_NODE_KINDS.has(node.kind) || node.kind === 'unresolved') {
    return 'Other';
  }
  const note = node.noteId ? notesById.get(node.noteId) : notesById.get(node.id);
  if (!note) {
    return 'Other';
  }
  switch (groupBy) {
    case 'type':
      return note.type || 'untyped';
    case 'tag':
      return note.tags?.[0] || 'untagged';
    case 'folder':
    default:
      return folderOf(note.path);
  }
}

// Resolve a force-graph link endpoint (an id string before the sim resolves it, or
// a node object after) to its node id.
function endId(end: unknown): string {
  return typeof end === 'object' && end ? (end as {id: string}).id : String(end);
}

// Cap the rendered graph to the most-connected nodes so a very large query still
// renders smoothly. The selected node is always kept; the rest are filled by
// descending degree (so hubs survive). Returns the trimmed graph plus how many
// nodes were dropped, for the "N more" badge.
function capGraph(data: GraphData, cap: number, selectedID: string): {data: GraphData; hidden: number} {
  if (data.nodes.length <= cap) {
    return {data, hidden: 0};
  }
  const keep = new Set<string>();
  if (selectedID) {
    for (const node of data.nodes) {
      if ((node.noteId ? node.noteId === selectedID : node.id === selectedID)) {
        keep.add(node.id);
      }
    }
  }
  const ranked = data.nodes.slice().sort((a, b) => b.degree - a.degree);
  for (const node of ranked) {
    if (keep.size >= cap) {
      break;
    }
    keep.add(node.id);
  }
  const nodes = data.nodes.filter((node) => keep.has(node.id));
  const links = data.links.filter((link) => keep.has(endId(link.source)) && keep.has(endId(link.target)));
  return {data: {nodes, links}, hidden: data.nodes.length - nodes.length};
}

// Collapse a large graph into one super-node per group (by the active group-by),
// with a single edge between any two groups that have a note-to-note link. Each
// super-node's id encodes the facet axis + key so a click can drill in. Hub /
// unresolved nodes are dropped from the overview.
function aggregateGraph(data: GraphData, groupBy: GraphViewState['groupBy'], notesById: Map<string, models.NoteSummaryDTO>): GraphData {
  const axis = GROUPBY_AXIS[groupBy ?? 'folder'] ?? 'folders';
  const counts = new Map<string, number>();
  const nodeGroup = new Map<string, string>();
  for (const node of data.nodes) {
    if (HUB_NODE_KINDS.has(node.kind) || node.kind === 'unresolved') {
      continue;
    }
    const key = groupKeyFor(node, groupBy, notesById);
    nodeGroup.set(node.id, key);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const nodes: GraphNode[] = [];
  for (const [key, count] of counts) {
    nodes.push({
      id: `agg:${axis}:${key}`,
      label: `${key} · ${count}`,
      kind: 'aggregate',
      val: Math.min(40, 8 + Math.sqrt(count) * 3),
      degree: count,
      selected: false,
    });
  }
  const seen = new Set<string>();
  const links: GraphLink[] = [];
  for (const link of data.links) {
    const s = nodeGroup.get(endId(link.source));
    const t = nodeGroup.get(endId(link.target));
    if (!s || !t || s === t) {
      continue;
    }
    const key = s < t ? `${s}|${t}` : `${t}|${s}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    links.push({source: `agg:${axis}:${s}`, target: `agg:${axis}:${t}`, kind: 'links_to', linkType: 'hard'});
  }
  return {nodes, links};
}

// True when a saved flat layout has collapsed to (nearly) a line — the elongation
// of its point cloud along its principal axes is extreme. Such a layout, restored
// and pinned, shows as the "diagonal stretch" bug; detecting it lets us respread
// instead of honouring it. Rotation-invariant (covariance eigenvalues), so a
// diagonal line is caught the same as an axis-aligned one.
function isDegenerateLayout(coords: Record<string, models.LayoutCoordinatesDTO>): boolean {
  const pts: Array<{x: number; y: number}> = [];
  for (const key in coords) {
    const c = coords[key];
    if (c && Number.isFinite(c.x) && Number.isFinite(c.y)) {
      pts.push({x: c.x, y: c.y});
    }
  }
  if (pts.length < 6) {
    return false; // too few points to judge; keep whatever was saved
  }
  let mx = 0;
  let my = 0;
  for (const p of pts) {
    mx += p.x;
    my += p.y;
  }
  mx /= pts.length;
  my /= pts.length;
  let sxx = 0;
  let syy = 0;
  let sxy = 0;
  for (const p of pts) {
    const dx = p.x - mx;
    const dy = p.y - my;
    sxx += dx * dx;
    syy += dy * dy;
    sxy += dx * dy;
  }
  const trace = sxx + syy;
  const det = sxx * syy - sxy * sxy;
  const disc = Math.sqrt(Math.max(0, (trace * trace) / 4 - det));
  const major = trace / 2 + disc; // variance along the principal axis
  const minor = trace / 2 - disc; // variance perpendicular to it
  if (major <= 1e-6) {
    return false; // all points coincident — not the stretch case
  }
  // Variance ratio > 12 ≈ a >3.5:1 spread — clearly a stretched near-line.
  return minor <= 1e-6 || major / minor > 12;
}


// Zoom slider ↔ camera distance (logarithmic). Slider 0 = zoomed out (far, at
// ZOOM_MAX_DISTANCE); 100 = zoomed in (near, at ZOOM_MIN_DISTANCE).
function zoomPctToDistance(pct: number): number {
  const t = Math.min(1, Math.max(0, pct / 100));
  const logMin = Math.log(ZOOM_MIN_DISTANCE);
  const logMax = Math.log(ZOOM_MAX_DISTANCE);
  return Math.exp(logMax - t * (logMax - logMin));
}
function zoomDistanceToPct(distance: number): number {
  const logMin = Math.log(ZOOM_MIN_DISTANCE);
  const logMax = Math.log(ZOOM_MAX_DISTANCE);
  const d = Math.min(ZOOM_MAX_DISTANCE, Math.max(ZOOM_MIN_DISTANCE, distance));
  const t = (logMax - Math.log(d)) / (logMax - logMin);
  return Math.round(Math.min(1, Math.max(0, t)) * 100);
}

function LegendDot({color, label}: {color: string; label: string}) {
  return (
    <span style={{display: 'flex', alignItems: 'center', gap: 6}}>
      <span style={{width: 9, height: 9, borderRadius: '50%', background: color, flex: '0 0 auto'}} />
      {label}
    </span>
  );
}

function LegendLine({color, label}: {color: string; label: string}) {
  return (
    <span style={{display: 'flex', alignItems: 'center', gap: 6}}>
      <span style={{width: 14, borderTop: `2px solid ${color}`, flex: '0 0 auto'}} />
      {label}
    </span>
  );
}

// Curated categorical palettes for zone groups. Chosen for mutual contrast so
// adjacent zones read as clearly distinct — the old hash-to-hue mapping tended to
// land neighbouring groups on near-identical, muddy hues. Indexed by a stable
// hash of the group key, so a group keeps its colour across renders.
const GROUP_PALETTE_LIGHT = [
  '#4c63e6', '#e0562f', '#2f9e6b', '#c44cb0', '#c9861f',
  '#1f9ec4', '#7c53e6', '#3f8f3f', '#d14d76', '#5a7a1f', '#0f8ea9', '#b0682f',
];
const GROUP_PALETTE_DARK = [
  '#7c8dff', '#f0765f', '#4fd39a', '#e07bd6', '#e8be4f',
  '#5fd0ec', '#a98cff', '#6fc46f', '#f07a9e', '#9fca4f', '#5fe0f0', '#e0985f',
];

// Deterministic categorical colour per group key (stable across renders).
function groupColor(key: string, theme: 'light' | 'dark'): string {
  let hash = 0;
  for (let i = 0; i < key.length; i += 1) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  const palette = theme === 'dark' ? GROUP_PALETTE_DARK : GROUP_PALETTE_LIGHT;
  return palette[hash % palette.length];
}

export default GraphView3D;
