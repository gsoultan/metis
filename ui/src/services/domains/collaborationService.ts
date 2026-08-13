import { requestJSON } from "../shared/rest";

/** A designer presence or edit event, relayed to everyone else on the diagram. */
export interface CollaborationBroadcast {
  type: string;
  projectId?: string | null;
  definitionId?: string | null;
  instanceId?: string | null;
  userId?: string;
  userName?: string;
  timestamp?: string;
  data?: Record<string, unknown>;
}

export const collaborationService = {
  async broadcastCollaboration(event: CollaborationBroadcast, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>("/collaboration/broadcast", {
      method: "POST",
      body: { event },
      signal,
    });

    return { err: data.err };
  },
};
