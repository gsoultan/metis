/**
 * Where a step added by a click lands, and whether it joins the selected one.
 *
 * Pure, so the rules can be tested without a canvas: the hook that adds the
 * step only supplies the diagram and the point it would otherwise use.
 */
import { isEndingNode } from './bpmnVocabulary';
import { DECIDE_GROUP_KIND } from './decideGroup';

/** A step's footprint before React Flow has measured it. */
export const DEFAULT_STEP_WIDTH = 180;
export const DEFAULT_STEP_HEIGHT = 70;
/** The room between a step and the one added after it, for the arrow. */
export const STEP_GAP = 90;

/** Kinds no arrow may reach: a start begins a path, the rest are not steps. */
const NO_ARROW_IN = new Set(['startEvent', 'boundaryEvent', 'pool', 'lane', DECIDE_GROUP_KIND]);
/** Kinds no arrow may leave, beside the ones that end a path. */
const NO_ARROW_OUT = new Set(['pool', 'lane']);

/** Whether a new step of kind `added` should follow a selected step of kind `selected`. */
export function followsOn(selected: string, added: string): boolean {
  return !isEndingNode(selected) && !NO_ARROW_OUT.has(selected) && !NO_ARROW_IN.has(added);
}

/** Whether anything can be added after a step of this kind — what the palette says. */
export function canFollow(selected: string): boolean {
  return !isEndingNode(selected) && !NO_ARROW_OUT.has(selected);
}

/** The parts of a canvas node placement reads. */
export interface PlacedNode {
  position: { x: number; y: number };
  measured?: { width?: number; height?: number };
  /** Set for a step drawn inside a sub-process; its position is relative to that. */
  parentId?: string;
}

/**
 * The nearest spot at or right of `position` that no top-level step covers.
 *
 * Three clicks in a row used to stack three steps on one spot, so the second
 * and third looked like they had not been added at all.
 */
export function clearSpot(position: { x: number; y: number }, nodes: PlacedNode[]): { x: number; y: number } {
  const covers = (spot: { x: number; y: number }) =>
    nodes.some((node) => {
      if (node.parentId) return false;
      const width = node.measured?.width ?? DEFAULT_STEP_WIDTH;
      const height = node.measured?.height ?? DEFAULT_STEP_HEIGHT;
      return (
        spot.x < node.position.x + width + 24 &&
        spot.x + DEFAULT_STEP_WIDTH + 24 > node.position.x &&
        spot.y < node.position.y + height &&
        spot.y + DEFAULT_STEP_HEIGHT > node.position.y
      );
    });
  let spot = position;
  for (let tries = 0; tries < 30 && covers(spot); tries++) {
    spot = { x: spot.x + DEFAULT_STEP_WIDTH / 2, y: spot.y };
  }
  return spot;
}
