// Zoned layout: cluster nodes by `group`, give each group its own region, and
// draw a rounded hull around each region so the grouping reads visually. Groups
// are arranged around a big circle (rather than a grid) because groups vary a lot
// in size — a ring lets each group claim an angular wedge whose radius grows with
// its member count, avoiding the fixed-cell waste/overflow of a grid.
import {convexHull, padHull, roundHull} from './hull';
import type {LayoutEngine, LayoutNode, LayoutResult, Point, Hull} from './types';

// Base radius of a group's packed disc per member, before spacing. The disc grows
// as sqrt(count) so area (not radius) scales with population.
const NODE_PACK_SPACING = 46;
// Extra breathing room between the global-ring radius and the largest group disc.
const GROUP_RING_PADDING = 1.35;
// Hull outward padding as a fraction of a node's pack spacing.
const HULL_PADDING = 34;
// Golden angle for the intra-group sunflower spiral — even, non-clumping packing.
const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5));

const OTHER_GROUP = 'Other';

export const zonedLayout: LayoutEngine = (nodes, _edges, opts): LayoutResult => {
  const spacing = opts?.spacing ?? 1;
  const positions: Record<string, Point> = {};

  // Partition into groups; ungrouped nodes collapse into a synthetic "Other".
  const groups = new Map<string, LayoutNode[]>();
  for (const node of nodes) {
    const key = node.group && node.group.length > 0 ? node.group : OTHER_GROUP;
    const bucket = groups.get(key);
    if (bucket) {
      bucket.push(node);
    } else {
      groups.set(key, [node]);
    }
  }

  // Deterministic group order (by key) so the same input always produces the same
  // wedge assignment.
  const groupKeys = Array.from(groups.keys()).sort();

  // Radius of a group's packed disc: sqrt(count) so area ∝ member count.
  const discRadius = (count: number) => Math.sqrt(count) * NODE_PACK_SPACING * spacing;

  // The global ring must be large enough that adjacent group discs don't overlap.
  // Take the largest disc and size the ring so its circumference comfortably seats
  // all groups; also enforce a floor from the summed diameters.
  const largestDisc = Math.max(0, ...groupKeys.map((k) => discRadius(groups.get(k)!.length)));
  const summedDiameter = groupKeys.reduce((acc, k) => acc + discRadius(groups.get(k)!.length) * 2, 0);
  const ringByCircumference = (summedDiameter * GROUP_RING_PADDING) / (Math.PI * 2);
  const ringRadius = groupKeys.length <= 1 ? 0 : Math.max(largestDisc * GROUP_RING_PADDING, ringByCircumference);

  const hulls: Hull[] = [];

  groupKeys.forEach((key, gi) => {
    const members = groups.get(key)!;
    // Group centroid: evenly spaced around the global ring. A single group sits at
    // the origin (ringRadius === 0).
    const angle = (gi / Math.max(1, groupKeys.length)) * Math.PI * 2;
    const cx = Math.cos(angle) * ringRadius;
    const cy = Math.sin(angle) * ringRadius;

    // Order members by descending degree so higher-degree nodes take the innermost
    // spiral slots (nearest the centroid); ties broken by id for determinism.
    const ordered = members
      .slice()
      .sort((a, b) => (b.degree - a.degree) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));

    // Sunflower / phyllotaxis packing: the i-th node sits at radius ∝ sqrt(i) and
    // angle i * goldenAngle, giving a compact, evenly-dense disc with no gaps.
    const pts: Point[] = [];
    ordered.forEach((node, i) => {
      const r = Math.sqrt(i) * NODE_PACK_SPACING * spacing;
      const a = i * GOLDEN_ANGLE;
      const p = {x: cx + Math.cos(a) * r, y: cy + Math.sin(a) * r};
      positions[node.id] = p;
      pts.push(p);
    });

    // Hull: convex hull of the packed points, pushed out by a padding margin and
    // smoothed into a rounded blob. Degenerate small groups are handled inside the
    // hull helpers (lone node → square/circle, pair → capsule).
    const padded = padHull(convexHull(pts), HULL_PADDING * spacing);
    hulls.push({group: key, points: roundHull(padded)});
  });

  return {positions, hulls};
};
