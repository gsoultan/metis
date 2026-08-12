import { useEffect, useState } from 'react';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

export interface CollaborationEvent {
  type: 'cursor' | 'node_move' | 'node_update' | 'presence';
  projectId: string;
  userId: string;
  userName: string;
  data: any;
  timestamp: string;
}

export function useCollaboration(projectId: string | undefined) {
  const { user } = useAppStore();
  const [remoteCursors, setRemoteCursors] = useState<Record<string, { x: number, y: number, name: string }>>({});
  const [remoteEvents, setRemoteEvents] = useState<CollaborationEvent[]>([]);

  useEffect(() => {
    if (!projectId) return;

    // Use existing SSE endpoint from ProcessDesigner or similar
    const eventSource = new EventSource(`${import.meta.env.VITE_API_BASE_URL || ''}/api/v1/sse`);

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.projectId === projectId && data.userId !== user?.id) {
          if (data.type === 'cursor') {
            setRemoteCursors(prev => ({
              ...prev,
              [data.userId]: { ...data.data, name: data.userName }
            }));
          } else {
            setRemoteEvents(prev => [...prev, data]);
          }
        }
      } catch {
        // Not a collaboration event or parse error
      }
    };

    return () => eventSource.close();
  }, [projectId, user?.id]);

  const broadcast = async (type: CollaborationEvent['type'], data: any) => {
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
