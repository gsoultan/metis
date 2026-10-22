import { afterEach, describe, expect, test } from "bun:test";
import { webhookService } from "./webhookService";
import { stubFetch } from "../shared/stubbedFetch";

const payload = { project_id: "p-1", name: "Invoices", message_name: "invoice-received" };

describe("webhookService writes", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("createWebhook raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ err: "message name is required" }));
    await expect(webhookService.createWebhook(payload)).rejects.toThrow("message name is required");
  });

  test("createWebhook sends an object, not a JSON string wrapped in JSON", async () => {
    const stub = stubFetch({ webhook: { id: "w-1" } });
    restore = stub.restore;
    await webhookService.createWebhook(payload);
    expect(stub.sent[0].body).toEqual(payload);
  });

  test("setWebhookEnabled raises the refusal", async () => {
    ({ restore } = stubFetch({ err: "webhook not found" }));
    await expect(webhookService.setWebhookEnabled("w-1", false)).rejects.toThrow("webhook not found");
  });

  test("closeLegacySignatures deletes the webhook's legacy window", async () => {
    const stub = stubFetch({});
    restore = stub.restore;
    await webhookService.closeLegacySignatures("w-1");
    expect(stub.sent[0].method).toBe("DELETE");
    expect(stub.sent[0].url).toEndWith("/webhooks/w-1/legacy-signatures");
  });

  test("closeLegacySignatures raises the refusal", async () => {
    ({ restore } = stubFetch({ err: "webhook not found" }));
    await expect(webhookService.closeLegacySignatures("w-1")).rejects.toThrow("webhook not found");
  });

  test("deleteWebhook raises the refusal", async () => {
    ({ restore } = stubFetch({ err: "webhook not found" }));
    await expect(webhookService.deleteWebhook("w-1")).rejects.toThrow("webhook not found");
  });
});

describe("webhookService simulation", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("simulateWebhookDelivery posts the body as text, with only the headers given", async () => {
    const simulation = { would_act: true, enabled: true, signature: { checked: false, valid: false }, message_name: "m", waiting: [], waiting_total: 0 };
    const stub = stubFetch({ simulation });
    restore = stub.restore;
    const body = '{"order":{"id":"PO-1"}}';
    expect(await webhookService.simulateWebhookDelivery("w-1", { body })).toEqual(simulation);
    expect(stub.sent[0].method).toBe("POST");
    expect(stub.sent[0].url).toEndWith("/webhooks/w-1/simulate");
    expect(stub.sent[0].body).toEqual({ body });
  });

  test("simulateWebhookDelivery raises the refusal", async () => {
    ({ restore } = stubFetch({ err: "no such webhook" }));
    await expect(webhookService.simulateWebhookDelivery("w-1", { body: "{}" })).rejects.toThrow("no such webhook");
  });
});
