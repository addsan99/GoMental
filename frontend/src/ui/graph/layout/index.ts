// Public entry point for the graph layout engines. Re-exports the shared types
// and resolves a LayoutKind to its engine. All engines share the LayoutEngine
// signature so the renderer can swap them freely.
export type {
  LayoutKind,
  Point,
  LayoutNode,
  LayoutEdge,
  Hull,
  LayoutResult,
  LayoutOptions,
  LayoutEngine,
} from './types';

export {radialLayout} from './radial';
export {zonedLayout} from './zoned';
export {forceLayout} from './force';
export {convexHull, padHull, roundHull} from './hull';

import type {LayoutEngine, LayoutKind} from './types';
import {radialLayout} from './radial';
import {zonedLayout} from './zoned';
import {forceLayout} from './force';

export function layoutEngineFor(kind: LayoutKind): LayoutEngine {
  switch (kind) {
    case 'radial':
      return radialLayout;
    case 'zoned':
      return zonedLayout;
    case 'force':
    default:
      return forceLayout;
  }
}
