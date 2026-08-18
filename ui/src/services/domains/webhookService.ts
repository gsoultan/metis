import { requestJSON } from "../shared/rest";

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

  async createWebhook(payload: CreateWebhookPayload, signal?: AbortSignal) {
    const data = await requestJSON<CreateWebhookResponse>("/webhooks", {
      method: "POST",
      body: JSON.stringify(payload),
      signal,
    });
    return data.webhook;
  },

  async setWebhookEnabled(id: string, enabled: boolean, signal?: AbortSignal) {
    return requestJSON<{ err?: string }>(`/webhooks/${id}/enabled`, {
      method: "POST",
      body: JSON.stringify({ enabled }),
      signal,
    });
  },

  async deleteWebhook(id: string, signal?: AbortSignal) {
    return requestJSON<{ err?: string }>(`/webhooks/${id}`, { method: "DELETE", signal });
  },
};
