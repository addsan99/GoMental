// Convex-hull helpers used by the zoned layout to draw a blob around each group.
// Pure geometry over Point[] — no dependencies. All functions are total: they
// return a sensible closed polygon even for degenerate 0/1/2-point inputs so a
// lone-node group still gets a visible shape.
import type {Point} from './types';

function centroid(points: Point[]): Point {
  let x = 0;
  let y = 0;
  for (const p of points) {
    x += p.x;
    y += p.y;
  }
  return {x: x / points.length, y: y / points.length};
}

// Andrew's monotone chain convex hull. Returns the hull vertices in
// counter-clockwise order (screen y-up math; the sign convention is internally
// consistent). Degenerate inputs (<3 points) are returned as-is (deduped) since
// there is no polygon to compute yet — callers (padHull/roundHull) handle those.
export function convexHull(points: Point[]): Point[] {
  // Dedupe and sort by x then y.
  const pts = points
    .slice()
    .sort((a, b) => (a.x === b.x ? a.y - b.y : a.x - b.x))
    .filter((p, i, arr) => i === 0 || p.x !== arr[i - 1].x || p.y !== arr[i - 1].y);

  if (pts.length < 3) {
    return pts;
  }

  const cross = (o: Point, a: Point, b: Point) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);

  const lower: Point[] = [];
  for (const p of pts) {
    while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], p) <= 0) {
      lower.pop();
    }
    lower.push(p);
  }

  const upper: Point[] = [];
  for (let i = pts.length - 1; i >= 0; i -= 1) {
    const p = pts[i];
    while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], p) <= 0) {
      upper.pop();
    }
    upper.push(p);
  }

  // Drop the last point of each list (it's the first point of the other).
  lower.pop();
  upper.pop();
  return lower.concat(upper);
}

// Push each vertex outward from the polygon centroid by `padding`. For the
// degenerate 0/1/2-point cases we synthesise a small shape so the group is still
// visible: a point becomes a padded square, two points become a padded capsule-
// ish quad spanning their axis.
export function padHull(points: Point[], padding: number): Point[] {
  if (points.length === 0) {
    return [];
  }
  if (points.length === 1) {
    return squareAround(points[0], padding);
  }
  if (points.length === 2) {
    return capsuleQuad(points[0], points[1], padding);
  }

  const c = centroid(points);
  return points.map((p) => {
    const dx = p.x - c.x;
    const dy = p.y - c.y;
    const len = Math.hypot(dx, dy) || 1;
    return {x: p.x + (dx / len) * padding, y: p.y + (dy / len) * padding};
  });
}

// A padding-sized square centred on a single point (fallback for lone nodes).
function squareAround(p: Point, padding: number): Point[] {
  const r = Math.max(padding, 1);
  return [
    {x: p.x - r, y: p.y - r},
    {x: p.x + r, y: p.y - r},
    {x: p.x + r, y: p.y + r},
    {x: p.x - r, y: p.y + r},
  ];
}

// A padded quad around the segment a–b: offset both endpoints along the segment
// and perpendicular to it, giving a rectangle-with-margin around the two nodes.
function capsuleQuad(a: Point, b: Point, padding: number): Point[] {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len = Math.hypot(dx, dy) || 1;
  const ux = dx / len;
  const uy = dy / len;
  const px = -uy; // perpendicular
  const py = ux;
  const r = Math.max(padding, 1);
  // Extend past each endpoint along the axis, then offset perpendicular on both
  // sides — order the four corners so the polygon is non-self-intersecting.
  const a0 = {x: a.x - ux * r, y: a.y - uy * r};
  const b0 = {x: b.x + ux * r, y: b.y + uy * r};
  return [
    {x: a0.x + px * r, y: a0.y + py * r},
    {x: b0.x + px * r, y: b0.y + py * r},
    {x: b0.x - px * r, y: b0.y - py * r},
    {x: a0.x - px * r, y: a0.y - py * r},
  ];
}

// Smooth a closed polygon into a rounded blob via Chaikin corner-cutting.
// `samples` controls how many refinement iterations (more = rounder). Degenerate
// inputs are routed through padHull-style fallbacks so a rounded shape always
// results. The returned polygon is closed (first !== last; caller closes it).
export function roundHull(points: Point[], samples = 3): Point[] {
  if (points.length === 0) {
    return [];
  }
  if (points.length === 1) {
    return circleAround(points[0], Math.max(4, 1), Math.max(8, samples * 4));
  }
  if (points.length === 2) {
    // Round the capsule quad rather than sampling a true stadium — good enough
    // for a visible two-node blob.
    return chaikin(capsuleQuad(points[0], points[1], 4), samples);
  }
  return chaikin(points, samples);
}

// One-or-more Chaikin passes on a closed polygon (each pass replaces every edge
// with two points at 1/4 and 3/4, cutting corners toward roundness).
function chaikin(polygon: Point[], iterations: number): Point[] {
  let poly = polygon;
  for (let it = 0; it < Math.max(0, iterations); it += 1) {
    const next: Point[] = [];
    for (let i = 0; i < poly.length; i += 1) {
      const p = poly[i];
      const q = poly[(i + 1) % poly.length];
      next.push({x: p.x * 0.75 + q.x * 0.25, y: p.y * 0.75 + q.y * 0.25});
      next.push({x: p.x * 0.25 + q.x * 0.75, y: p.y * 0.25 + q.y * 0.75});
    }
    poly = next;
  }
  return poly;
}

// A regular polygon approximating a circle (fallback for a single point).
function circleAround(p: Point, radius: number, segments: number): Point[] {
  const out: Point[] = [];
  for (let i = 0; i < segments; i += 1) {
    const angle = (i / segments) * Math.PI * 2;
    out.push({x: p.x + Math.cos(angle) * radius, y: p.y + Math.sin(angle) * radius});
  }
  return out;
}
