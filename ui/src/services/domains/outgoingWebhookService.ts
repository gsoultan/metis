import { requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** The names a receiver asks for, as the server spells them. */
export type OutgoingWebhookEvent =
  | "process.started"
  | "process.completed"
  | "task.created"
  | "task.completed"
  | "incident.raised";

/** Mirrors outgoingwebhook.WebhookView. */
export interface ApiOutgoingWebhook {
  id: string;
  project_id: string;
  name: string;
  url: string;
  events: OutgoingWebhookEvent[];
  enabled: boolean;
  created_at: string;
  /**
   * Returned exactly once, by create. It is encrypted at rest and no read path
   * gives it back, so if it is not kept at that moment it is gone.
   */
  secret?: string;
}

export type DeliveryStatus = "pending" | "delivered" | "failed";

/** Mirrors outgoingwebhook.DeliveryView: one delivery in a webhook's log. */
export interface ApiOutgoingDelivery {
  id: string;
  webhook_id: string;
  event: OutgoingWebhookEvent;
  status: DeliveryStatus;
  attempts: number;
  /** Only while the delivery is pending. */
  next_attempt_at?: string;
  last_attempt_at?: string;
  /** Absent when the last attempt had no answer. */
  last_status_code?: number;
  last_error?: string;
  last_response?: string;
  test?: boolean;
  /** The exact JSON that was posted. */
  body: unknown;
  created_at: string;
}

/** Mirrors outgoingwebhook.TestView: a test as it went out and came back. */
export interface ApiOutgoingTest {
  delivery_id: string;
  event: OutgoingWebhookEvent;
  accepted: boolean;
  duration_ms: number;
  request: { method: string; url: string; headers: Record<string, string>; body: unknown };
  response: { status_code?: number; body?: string; error?: string };
}

export interface CreateOutgoingWebhookPayload {
  name: string;
  url: string;
  events: OutgoingWebhookEvent[];
}

export interface DeliveryPageQuery {
  status?: DeliveryStatus;
  /** The cursor the previous page answered with, to read on from there. */
  before?: string;
  limit?: number;
}

export interface DeliveryPage {
  deliveries: ApiOutgoingDelivery[];
  /** Absent on the last page. */
  next_before?: string;
}

type ListResponse = { webhooks?: ApiOutgoingWebhook[]; err?: string };
type CreateResponse = { webhook?: ApiOutgoingWebhook; err?: string };
type TestResponse = { test?: ApiOutgoingTest; err?: string };
type DeliveriesResponse = { deliveries?: ApiOutgoingDelivery[]; next_before?: string; err?: string };

const projectPath = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/outgoing-webhooks`;
const webhookPath = (id: string) => `/outgoing-webhooks/${encodeURIComponent(id)}`;
const deliveryPath = (id: string) => `/outgoing-webhook-deliveries/${encodeURIComponent(id)}`;

export const outgoingWebhookService = {
  async listOutgoingWebhooks(projectId: string, signal?: AbortSignal) {
    const data = await requestJSON<ListResponse>(projectPath(projectId), { signal });
    return raiseIfRefused(data).webhooks ?? [];
  },

  async createOutgoingWebhook(projectId: string, payload: CreateOutgoingWebhookPayload, signal?: AbortSignal) {
    const data = await requestJSON<CreateResponse>(projectPath(projectId), { method: "POST", body: payload, signal });
    const { webhook } = raiseIfRefused(data);
    if (!webhook) throw new Error("The server did not return the webhook.");
    return webhook;
  },

  async setOutgoingWebhookEnabled(id: string, enabled: boolean, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`${webhookPath(id)}/enabled`, {
      method: "POST",
      body: { enabled },
      signal,
    });
    raiseIfRefused(data);
  },

  async deleteOutgoingWebhook(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(webhookPath(id), { method: "DELETE", signal });
    raiseIfRefused(data);
  },

  /** Posts a signed sample now and waits for the receiver's answer. */
  async sendOutgoingWebhookTest(id: string, event?: OutgoingWebhookEvent, signal?: AbortSignal) {
    const data = await requestJSON<TestResponse>(`${webhookPath(id)}/test`, {
      method: "POST",
      body: event ? { event } : {},
      signal,
    });
    const { test } = raiseIfRefused(data);
    if (!test) throw new Error("The server did not say how the test went.");
    return test;
  },

  async listOutgoingDeliveries(id: string, query: DeliveryPageQuery = {}, signal?: AbortSignal): Promise<DeliveryPage> {
    const params = new URLSearchParams();
    if (query.status) params.set("status", query.status);
    if (query.before) params.set("before", query.before);
    if (query.limit) params.set("limit", String(query.limit));
    const search = params.size > 0 ? `?${params.toString()}` : "";
    const data = await requestJSON<DeliveriesResponse>(`${webhookPath(id)}/deliveries${search}`, { signal });
    const page = raiseIfRefused(data);
    return { deliveries: page.deliveries ?? [], next_before: page.next_before || undefined };
  },

  async retryOutgoingDelivery(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`${deliveryPath(id)}/retry`, { method: "POST", signal });
    raiseIfRefused(data);
  },

  async stopOutgoingDelivery(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`${deliveryPath(id)}/stop`, { method: "POST", signal });
    raiseIfRefused(data);
  },
};
