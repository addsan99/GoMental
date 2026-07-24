// Colour + prominence helpers shared by the graph renderer. These are pure
// functions of a node/link's attributes and the active theme, so they can be
// reused across the 2D (flat) and 3D instances without touching component state.
import type {LinkType} from './model';

export const DEPTH_OPTIONS = [1, 2, 3];
// Deepest ring the depth-shaded colouring is normalised against. Fixed (not the
// per-graph max) so a node at a given hop is the same colour regardless of the
// depth setting — e.g. a 2-hop node looks identical at depth=2 and depth=3.
export const MAX_DEPTH_HOPS = DEPTH_OPTIONS[DEPTH_OPTIONS.length - 1];

export const HUB_NODE_KINDS = new Set(['tag', 'type', 'heading']);

// Solid link palette (matches the 2D legend swatches): hard = indigo/blue,
// soft = amber, metadata = magenta.
export function edgeColor(type: LinkType, theme: 'light' | 'dark'): string {
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

export function nodeColor(kind: string, selected: boolean): string {
  if (selected) {
    return '#ef4444';
  }
  if (HUB_NODE_KINDS.has(kind)) {
    return '#b054c4';
  }
  return kind === 'unresolved' ? '#8b8170' : '#2f8f9e';
}

// Fraction (0..1) of how connected a node is — log-scaled so it saturates for
// hubs and a handful of very high-degree nodes don't wash out the gradient.
export function nodeProminence(degree: number): number {
  return Math.min(1, Math.log2(degree + 1) / 6);
}

// Linear blend between two #rrggbb colours (t clamped to 0..1).
export function mixHex(a: string, b: string, t: number): string {
  const clamp = Math.max(0, Math.min(1, t));
  const pa = [parseInt(a.slice(1, 3), 16), parseInt(a.slice(3, 5), 16), parseInt(a.slice(5, 7), 16)];
  const pb = [parseInt(b.slice(1, 3), 16), parseInt(b.slice(3, 5), 16), parseInt(b.slice(5, 7), 16)];
  const mix = pa.map((v, i) => Math.round(v + (pb[i] - v) * clamp));
  return `#${mix.map((v) => v.toString(16).padStart(2, '0')).join('')}`;
}

// Regular-note fill shaded by connectivity: muted teal for leaves brightening
// toward vivid cyan for hub-like notes, so prominence reads in colour as well as
// size. Hubs (tag/type/heading) and unresolved nodes keep their fixed hues.
export function noteShade(degree: number, theme: 'light' | 'dark'): string {
  const dark = theme === 'dark';
  return mixHex(dark ? '#2f7f8c' : '#2f8f9e', dark ? '#5fe0f0' : '#0fa9c4', nodeProminence(degree));
}

// Regular-note fill shaded by distance from the selected node: nodes one hop out
// are bright, and each further ring is progressively darker, so proximity to the
// centre reads at a glance. `hops` is 1-based here (the selected node itself is
// drawn in the accent); maxHops is the deepest ring present, so the full gradient
// is used regardless of the depth setting. Falls back to a single bright shade
// when there is only one ring.
export function depthShade(hops: number, theme: 'light' | 'dark'): string {
  const dark = theme === 'dark';
  // Ramp from a vivid near colour to a genuinely dark shade — the far end shifts
  // hue toward deep navy (not just a dimmer teal) so the drop reads as a darker
  // colour. The 0.7 exponent front-loads the darkening so even the second ring is
  // clearly darker rather than the change bunching up at the far rings. Normalised
  // against a FIXED max (MAX_DEPTH_HOPS) so each hop maps to a stable colour no
  // matter the depth setting.
  const near = dark ? '#5fe0f0' : '#2f8f9e';
  const far = dark ? '#182742' : '#0d1830';
  const span = Math.max(1, MAX_DEPTH_HOPS - 1);
  const raw = Math.min(1, Math.max(0, (hops - 1) / span));
  return mixHex(near, far, Math.pow(raw, 0.7));
}

// Label ink for the sprite text: selected pops in the accent, hubs take the
// metadata hue, everything else a muted foreground that stays readable against
// the themed background without competing with the coloured spheres.
export function labelColor(kind: string, selected: boolean, theme: 'light' | 'dark'): string {
  if (selected) {
    return '#ef4444';
  }
  if (HUB_NODE_KINDS.has(kind)) {
    return theme === 'dark' ? '#d59ae4' : '#9a3fb0';
  }
  return theme === 'dark' ? '#c9c6d0' : '#5a564f';
}

// Background matches the app surface so the canvas doesn't punch a hole in the
// theme. Slightly deeper than the 2D dotted canvas to give the depth cue room.
export function backgroundColor(theme: 'light' | 'dark'): string {
  return theme === 'dark' ? '#141419' : '#f7f5f0';
}
