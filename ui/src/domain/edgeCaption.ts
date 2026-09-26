import type { Edge } from '@xyflow/react';

import type { BPMNEdgeData } from '../types/bpmn';

/**
 * An edge with some of its settings changed.
 *
 * A sequence flow has no name on the server, so the arrow is captioned with
 * its condition, and a change to the condition changes the caption with it,
 * a removed condition included. The designer used to take the caption from
 * the condition and, with none, fall back to the caption the arrow already
 * had, so deleting a condition in the raw editor or clearing it in the
 * builder left it on the canvas.
 *
 * A change that leaves the condition alone leaves the caption alone. A
 * template captions its arrows "Approved" and "Rejected" and leaves the
 * conditions to the author, and a note written first keeps that caption.
 */
export function edgeWithData<E extends Edge<BPMNEdgeData>>(edge: E, patch: Partial<BPMNEdgeData>): E {
  const data = { ...edge.data, ...patch };
  if (!Object.hasOwn(patch, 'condition')) return { ...edge, data };
  return { ...edge, data, label: typeof patch.condition === 'string' ? patch.condition : '' };
}
