import { afterEach, describe, expect, test } from "bun:test";
import { outgoingWebhookService } from "./outgoingWebhookService";
import { stubFetch } from "../shared/stubbedFetch";

describe("outgoingWebhookService", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("createOutgoingWebhook posts the form to the project and hands back the webhook with its secret", async () => {
    const stub = stubFetch({ webhook: { id: "o-1", secret: "s3cret" } });
    restore = stub.restore;
    const payload = { name: "Ledger", url: "https://ledger.example.com/hooks", events: ["process.completed" as const] };
    const hook = await outgoingWebhookService.createOutgoingWebhook("p 1", payload);
    expect(hook.secret).toBe("s3cret");
    expect(stub.sent[0].method).toBe("POST");
    expect(stub.sent[0].url).toEndWith("/projects/p%201/outgoing-webhooks");
    expect(stub.sent[0].body).toEqual(payload);
  });

  test("createOutgoingWebhook raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ err: "the address is not https" }));
    await expect(
      outgoingWebhookService.createOutgoingWebhook("p-1", { name: "x", url: "http://x", events: [] }),
    ).rejects.toThrow("the address is not https");
  });

  test("sendOutgoingWebhookTest names the event when one is chosen, and sends an empty object when not", async () => {
    const stub = stubFetch({ test: { delivery_id: "d-1", accepted: true } });
    restore = stub.restore;
    await outgoingWebhookService.sendOutgoingWebhookTest("o-1", "incident.raised");
    await outgoingWebhookService.sendOutgoingWebhookTest("o-1");
    expect(stub.sent[0].url).toEndWith("/outgoing-webhooks/o-1/test");
    expect(stub.sent[0].body).toEqual({ event: "incident.raised" });
    expect(stub.sent[1].body).toEqual({});
  });

  test("listOutgoingDeliveries asks for a status and a page, and reads the last page as having no next", async () => {
    const stub = stubFetch({ deliveries: [{ id: "d-1" }], next_before: "" });
    restore = stub.restore;
    const page = await outgoingWebhookService.listOutgoingDeliveries("o-1", { status: "failed", before: "abc=" });
    const url = new URL(stub.sent[0].url, "https://metis.example");
    expect(url.pathname).toEndWith("/outgoing-webhooks/o-1/deliveries");
    expect(url.searchParams.get("status")).toBe("failed");
    expect(url.searchParams.get("before")).toBe("abc=");
    expect(page.deliveries).toHaveLength(1);
    expect(page.next_before).toBeUndefined();
  });

  test("retry and stop post to the delivery, and raise a refusal", async () => {
    const stub = stubFetch({});
    restore = stub.restore;
    await outgoingWebhookService.retryOutgoingDelivery("d-1");
    await outgoingWebhookService.stopOutgoingDelivery("d-1");
    expect(stub.sent.map((s) => `${s.method} ${new URL(s.url, "https://metis.example").pathname}`)).toEqual([
      "POST /api/v1/outgoing-webhook-deliveries/d-1/retry",
      "POST /api/v1/outgoing-webhook-deliveries/d-1/stop",
    ]);
    stub.restore();
    ({ restore } = stubFetch({ err: "this delivery has already failed; retry it instead" }));
    await expect(outgoingWebhookService.stopOutgoingDelivery("d-1")).rejects.toThrow("already failed");
  });
});
