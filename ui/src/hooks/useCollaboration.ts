import { useEffect, useState } from 'react';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import { subscribeToEvents } from './useEventStream';

export interface CollaborationEvent {
  type: 'cursor' | 'node_move' | 'node_update' | 'presence';
  projectId: string;
  userId: string;
  userName: string;
  data: Record<string, unknown>;
  timestamp: string;
}

const COLLABORATION_EVENT_TYPES: CollaborationEvent['type'][] = ['cursor', 'node_move', 'node_update', 'presence'];

/**
 * How many remote edits are kept. The list was appended to for as long as the
 * designer stayed open — every node move by every collaborator, forever — and
 * the consumer re-walks the whole list on each change. The last few hundred is
 * what "recent activity" means; anything older has already been applied.
 */
export const MAX_REMOTE_EVENTS = 200;

export function useCollaboration(projectId: string | undefined) {
  const user = useAppStore((state) => state.user);
  const [remoteCursors, setRemoteCursors] = useState<Record<string, { x: number, y: number, name: string }>>({});
  const [remoteEvents, setRemoteEvents] = useState<CollaborationEvent[]>([]);

  useEffect(() => {
    if (!projectId) return;

    // Collaboration broadcasts travel on the same authenticated stream as the
    // engine's events; `/api/v1/sse`, which this used to open, never existed.
    return subscribeToEvents(COLLABORATION_EVENT_TYPES, (event) => {
      const data = event as unknown as CollaborationEvent;
      if (data.projectId !== projectId || data.userId === user?.id) return;

      if (data.type === 'cursor') {
        setRemoteCursors(prev => ({
          ...prev,
          [data.userId]: { ...(data.data as { x: number; y: number }), name: data.userName }
        }));
        return;
      }
      setRemoteEvents(prev => [...prev, data].slice(-MAX_REMOTE_EVENTS));
    });
  }, [projectId, user?.id]);

  const broadcast = async (type: CollaborationEvent['type'], data: Record<string, unknown>) => {
    if (!projectId || !user) return;

    await processService.broadcastCollaboration({
      type,
      projectId,
      userId: user.id,
      userName: user.name,
      data,
      timestamp: new Date().toISOString(),
    });
  };

  return { remoteCursors, remoteEvents, broadcast };
}
