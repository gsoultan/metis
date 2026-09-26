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

  async deleteWebhook(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/webhooks/${id}`, { method: "DELETE", signal });
    return { err: raiseIfRefused(data).err };
  },
};
