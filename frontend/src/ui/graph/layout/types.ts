// Shared types for the pluggable graph layout engines. These are pure,
// framework-agnostic value types — no React, no Three.js, no d3 — so the
// engines are ordinary deterministic math over {nodes, edges}. The renderer
// adapts its own GraphNode/GraphLink model into these before calling an engine.

export type LayoutKind = 'force' | 'radial' | 'zoned';

export interface Point {
  x: number;
  y: number;
}

export interface LayoutNode {
  id: string;
  degree: number; // connection count (for sizing/ordering)
  selected: boolean; // the focus node
  hops?: number; // undirected hop distance from focus (radial); undefined = unreachable
  group?: string; // grouping key for zoned (e.g. note type or folder); undefined = ungrouped
}

export interface LayoutEdge {
  source: string;
  target: string;
}

export interface Hull {
  group: string;
  points: Point[]; // closed polygon around a group
}

export interface LayoutResult {
  positions: Record<string, Point>;
  hulls?: Hull[];
}

export interface LayoutOptions {
  spacing?: number; // multiplier (default 1) scaling all distances
}

export type LayoutEngine = (nodes: LayoutNode[], edges: LayoutEdge[], opts?: LayoutOptions) => LayoutResult;
