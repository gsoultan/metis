import { createContext, useContext } from 'react';

import type { BPMNNodeData } from '../../types/bpmn';

/**
 * What a step on the canvas can do to the diagram from its own pad.
 *
 * Nodes are rendered by React Flow, not by the designer, so the designer's
 * handlers reach them through context. Outside the designer — the read-only
 * diagram preview — there is no provider and the pad offers nothing.
 */
export interface NodeActions {
  addAfter: (nodeId: string, type: string, data?: Partial<BPMNNodeData>) => void;
  remove: (nodeId: string) => void;
}

export const NodeActionsContext = createContext<NodeActions | null>(null);

export function useNodeActions(): NodeActions | null {
  return useContext(NodeActionsContext);
}
