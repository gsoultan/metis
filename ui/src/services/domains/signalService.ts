import { signalClient } from "../shared/connect";
import type { ProcessVariables } from "../types";
import { raiseIfRefused } from "../raise";

export const signalService = {
  async broadcastSignal(projectId: string, signalName: string, variables: ProcessVariables = {}, signal?: AbortSignal) {
    const response = await signalClient.broadcastSignal({ projectId, signalName, variables }, { signal });
    return { err: raiseIfRefused(response).error };
  },

  async sendMessage(projectId: string, messageName: string, correlationKey: string, variables: ProcessVariables = {}, signal?: AbortSignal) {
    const response = await signalClient.sendMessage({ projectId, messageName, correlationKey, variables }, { signal });
    return { err: raiseIfRefused(response).error };
  },
};
