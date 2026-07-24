// Radial (concentric-ring) layout: the focus node sits at the origin and every
// other node lands on a ring chosen by its undirected hop distance from focus.
// Purely deterministic — nodes are sorted by (group, id) so grouped neighbours
// stay angularly adjacent, and each ring's start angle is offset so successive
// rings don't line their nodes up along the same radial spokes.
import type {LayoutEngine, LayoutNode, LayoutResult, Point} from './types';

// Base gap between consecutive rings, before the spacing multiplier. Chosen so a
// typical node radius (a handful of px) fits comfortably between rings without
// them crowding; scaled by opts.spacing.
const RING_GAP = 140;

// Fraction of a full turn to rotate each successive ring's starting angle. An
// irrational-ish fraction (golden-angle derived) keeps nodes on adjacent rings
// from stacking radially even when ring populations share common factors.
const RING_ANGLE_OFFSET = 0.61803398875; // golden ratio fractional part, in turns

// Sort key so nodes sharing a group cluster together on a ring (grouped first by
// group, then by id for a stable deterministic order). Ungrouped (undefined)
// groups sort after named groups.
function ringOrder(a: LayoutNode, b: LayoutNode): number {
  const ga = a.group ?? '￿';
  const gb = b.group ?? '￿';
  if (ga !== gb) {
    return ga < gb ? -1 : 1;
  }
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

export const radialLayout: LayoutEngine = (nodes, _edges, opts): LayoutResult => {
  const spacing = opts?.spacing ?? 1;
  const positions: Record<string, Point> = {};

  // Bucket nodes by ring index. hops===0 (or the selected node) is the centre;
  // reachable nodes go to ring = hops; unreachable (undefined hops) are collected
  // separately and placed on a single ring beyond the furthest reachable one.
  const rings = new Map<number, LayoutNode[]>();
  const unreachable: LayoutNode[] = [];
  let maxHop = 0;

  for (const node of nodes) {
    if (node.selected || node.hops === 0) {
      positions[node.id] = {x: 0, y: 0};
      continue;
    }
    if (node.hops === undefined) {
      unreachable.push(node);
      continue;
    }
    const ring = rings.get(node.hops);
    if (ring) {
      ring.push(node);
    } else {
      rings.set(node.hops, [node]);
    }
    if (node.hops > maxHop) {
      maxHop = node.hops;
    }
  }

  // Place one ring: distribute its nodes evenly around the circle at the given
  // radius, ordered by (group, id) and rotated by ringIndex * offset.
  const placeRing = (members: LayoutNode[], radius: number, ringIndex: number) => {
    if (members.length === 0) {
      return;
    }
    members.sort(ringOrder);
    const step = (Math.PI * 2) / members.length;
    const start = ringIndex * RING_ANGLE_OFFSET * Math.PI * 2;
    for (let i = 0; i < members.length; i += 1) {
      const angle = start + i * step;
      positions[members[i].id] = {
        x: Math.cos(angle) * radius,
        y: Math.sin(angle) * radius,
      };
    }
  };

  // Reachable rings, in ascending hop order for stable ring-index offsets.
  const hopKeys = Array.from(rings.keys()).sort((a, b) => a - b);
  for (const hop of hopKeys) {
    placeRing(rings.get(hop)!, hop * RING_GAP * spacing, hop);
  }

  // Unreachable nodes form an outer ring one gap beyond the deepest reachable one.
  if (unreachable.length > 0) {
    const outerIndex = maxHop + 1;
    placeRing(unreachable, outerIndex * RING_GAP * spacing, outerIndex);
  }

  return {positions};
};
