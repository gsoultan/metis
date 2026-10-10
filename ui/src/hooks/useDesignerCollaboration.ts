/**
 * useDesignerCollaboration — wraps the collaboration WebSocket channel for the
 * BPMN designer.
 *
 * FE-ARCH-5: extracted from useProcessDesigner so collaboration concerns are
 * isolated.  Handles:
 *   - Broadcasting the local cursor position to other participants
 *   - Applying remote node-move / node-update events to the local canvas
 */
import { useCallback, useEffect, useRef } from 'react';
import type { Node, Edge } from '@xyflow/react';
import type { ReactFlowInstance } from '@xyflow/react';
import { useCollaboration, type CollaborationEvent } from './useCollaboration';
import { createCursorThrottle, type CursorThrottle } from './cursorThrottle';
import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';

interface UseDesignerCollaborationParams {
  projectId: string | null | undefined;
  reactFlowInstance: ReactFlowInstance<Node<BPMNNodeData>, Edge<BPMNEdgeData>> | null;
  setNodes: React.Dispatch<React.SetStateAction<Node<BPMNNodeData>[]>>;
}

interface UseDesignerCollaborationReturn {
  remoteCursors: ReturnType<typeof useCollaboration>['remoteCursors'];
  broadcast: ReturnType<typeof useCollaboration>['broadcast'];
  /** Cursor-move handler — attach to the ReactFlow `onMouseMove` prop. */
  onMouseMove: (event: React.MouseEvent) => void;
}

export function useDesignerCollaboration({
  projectId,
  reactFlowInstance,
  setNodes,
}: UseDesignerCollaborationParams): UseDesignerCollaborationReturn {
  const { remoteCursors, remoteEvents, broadcast } = useCollaboration(projectId ?? undefined);

  // Apply incoming events from remote participants to the local canvas.
  //
  // Only the ones not applied yet. The list keeps the last few hundred, and
  // walking all of it on each new event re-applied every old move over the
  // person's own later edits, one setNodes per entry.
  const lastApplied = useRef<CollaborationEvent | null>(null);
  useEffect(() => {
    const from = lastApplied.current ? remoteEvents.lastIndexOf(lastApplied.current) + 1 : 0;
    const fresh = remoteEvents.slice(from);
    if (fresh.length === 0) return;
    lastApplied.current = fresh[fresh.length - 1];
    fresh.forEach((event) => {
      if (event.type === 'node_move') {
        const { id, position } = event.data as { id: string; position: { x: number; y: number } };
        setNodes((nds) => nds.map((n) => (n.id === id ? { ...n, position } : n)));
      } else if (event.type === 'node_update') {
        const { id, data: nodeData } = event.data as { id: string; data: Record<string, unknown> };
        setNodes((nds) =>
          nds.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...nodeData } as BPMNNodeData } : n)),
        );
      }
    });
  }, [remoteEvents, setNodes]);

  // The latest broadcast, so the throttle made once keeps sending through
  // whichever project the designer is on now.
  const broadcastRef = useRef(broadcast);
  useEffect(() => {
    broadcastRef.current = broadcast;
  }, [broadcast]);
  const cursorThrottle = useRef<CursorThrottle | null>(null);
  useEffect(() => {
    const throttle = createCursorThrottle((position) => broadcastRef.current('cursor', position));
    cursorThrottle.current = throttle;
    return () => {
      throttle.cancel();
      cursorThrottle.current = null;
    };
  }, []);

  const onMouseMove = useCallback(
    (event: React.MouseEvent) => {
      if (!reactFlowInstance) return;
      const { x, y } = reactFlowInstance.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });
      cursorThrottle.current?.move({ x, y });
    },
    [reactFlowInstance],
  );

  return { remoteCursors, broadcast, onMouseMove };
}

