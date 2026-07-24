// Minimal ambient types for the subset of d3-force-3d we use. The package ships
// no declarations; react-force-graph-3d bundles it, and we reach for forceRadial
// to give the "radial" layout a soft hop-distance ring force. Only the members we
// actually call are typed; nodes are the force-graph's own node objects.
declare module 'd3-force-3d' {
  interface RadialForce<Node> {
    (alpha: number): void;
    initialize(nodes: Node[], ...args: unknown[]): void;
    strength(strength: number | ((node: Node, i: number, nodes: Node[]) => number)): RadialForce<Node>;
    radius(radius: number | ((node: Node, i: number, nodes: Node[]) => number)): RadialForce<Node>;
    x(x: number): RadialForce<Node>;
    y(y: number): RadialForce<Node>;
    z(z: number): RadialForce<Node>;
  }

  export function forceRadial<Node = {id: string; x?: number; y?: number; z?: number}>(
    radius: number | ((node: Node, i: number, nodes: Node[]) => number),
    x?: number,
    y?: number,
    z?: number,
  ): RadialForce<Node>;

  interface PositionForce<Node> {
    (alpha: number): void;
    initialize(nodes: Node[], ...args: unknown[]): void;
    strength(strength: number | ((node: Node, i: number, nodes: Node[]) => number)): PositionForce<Node>;
    x(x: number | ((node: Node, i: number, nodes: Node[]) => number)): PositionForce<Node>;
    y(y: number | ((node: Node, i: number, nodes: Node[]) => number)): PositionForce<Node>;
  }

  export function forceX<Node = {x?: number}>(x?: number): PositionForce<Node>;
  export function forceY<Node = {y?: number}>(y?: number): PositionForce<Node>;
}
