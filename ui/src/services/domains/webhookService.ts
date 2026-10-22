import { requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** Mirrors entities.Webhook. */
export interface ApiWebhook {
  id: string;
  name: string;
  /** Appears in the delivery URL. Identifies, does not authenticate. */
  token: string;
  /**
   * Returned exactly once, by create. It is encrypted at rest and no read path
   * gives it back, so if it is not kept at that moment it is gone.
   */
  secret?: string;
  signature_header?: string;
  message_name: string;
  correlation_expression?: string;
  enabled: boolean;
  /**
   * When this webhook stops accepting legacy signatures, which cover the body
   * alone. Absent for a webhook that accepts v2 only — every one created since
   * v2 existed.
   */
  legacy_signatures_until?: string;
}

/** Mirrors entities.WebhookSimulationWaiting: an instance a real delivery would move on. */
export interface ApiSimulatedWaiting {
  instance_id: string;
  process_key?: string;
  process_name?: string;
  business_key?: string;
  node_id: string;
  node_name?: string;
  since: string;
}

/** Mirrors entities.WebhookSimulationStart: a process a real delivery would start. */
export interface ApiSimulatedStart {
  process_key: string;
  process_name?: string;
  node_name?: string;
}

/**
 * Mirrors entities.WebhookSimulationResult. Nothing it describes happened: a
 * simulation moves no process and does not record the delivery ID.
 */
export interface ApiWebhookSimulation {
  would_act: boolean;
  /** What a real delivery would be answered with, when it would be refused. */
  refusal?: string;
  enabled: boolean;
  signature: { checked: boolean; valid: boolean; problem?: string };
  message_name: string;
  correlation_expression?: string;
  correlation_key?: string;
  /** Why the body could not be read, or the correlation rule found nothing in it. */
  body_problem?: string;
  already_received?: boolean;
  /** The first few waiting instances; waiting_total is how many there are. */
  waiting: ApiSimulatedWaiting[];
  waiting_total: number;
  starts?: ApiSimulatedStart[];
}

/** A delivery as the partner would send it. The signature fields are optional. */
export interface SimulateDeliveryPayload {
  body: string;
  timestamp?: string;
  delivery_id?: string;
  signature?: string;
}

type ListWebhooksResponse = { webhooks?: ApiWebhook[]; err?: string };
type CreateWebhookResponse = { webhook?: ApiWebhook; err?: string };

export interface CreateWebhookPayload {
  project_id: string;
  name: string;
  message_name: string;
  correlation_expression?: string;
  signature_header?: string;
}

export const webhookService = {
  async listWebhooks(projectId: string, signal?: AbortSignal) {
    const data = await requestJSON<ListWebhooksResponse>(
      `/webhooks?project_id=${encodeURIComponent(projectId)}`,
      { signal },
    );
    return { webhooks: data.webhooks ?? [], err: data.err };
  },

  // requestJSON serialises the body itself. These used to hand it a string
  // that was already JSON, so the server received a quoted string where it
  // expected an object and refused every create and every enable/disable.
  async createWebhook(payload: CreateWebhookPayload, signal?: AbortSignal) {
    const data = await requestJSON<CreateWebhookResponse>("/webhooks", {
      method: "POST",
      body: payload,
      signal,
    });
    return raiseIfRefused(data).webhook;
  },

  async setWebhookEnabled(id: string, enabled: boolean, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/webhooks/${id}/enabled`, {
      method: "POST",
      body: { enabled },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  /**
   * Stops a webhook accepting legacy signatures from now. The server only ever
   * shortens a window, so this cannot extend one.
   */
  async closeLegacySignatures(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/webhooks/${id}/legacy-signatures`, { method: "DELETE", signal });
    return { err: raiseIfRefused(data).err };
  },

  /** Runs a pasted delivery through every check a real one meets, changing nothing. */
  async simulateWebhookDelivery(id: string, payload: SimulateDeliveryPayload, signal?: AbortSignal) {
    const data = await requestJSON<{ simulation?: ApiWebhookSimulation; err?: string }>(`/webhooks/${id}/simulate`, {
      method: "POST",
      body: payload,
      signal,
    });
    const { simulation } = raiseIfRefused(data);
    if (!simulation) throw new Error("The server did not say what the delivery would do.");
    return simulation;
  },

  async deleteWebhook(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/webhooks/${id}`, { method: "DELETE", signal });
    return { err: raiseIfRefused(data).err };
  },
};
