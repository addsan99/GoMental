// Force layout is a deliberate no-op. In 'force' mode the renderer runs its own
// live d3-force simulation (react-force-graph), which continuously positions
// nodes frame-to-frame. Returning empty positions signals "don't seed anything —
// let the live sim own the layout", so a static precomputed layout can't fight
// the simulation. Kept as an engine so layoutEngineFor(kind) is total.
import type {LayoutEngine, LayoutResult} from './types';

export const forceLayout: LayoutEngine = (): LayoutResult => {
  return {positions: {}};
};
