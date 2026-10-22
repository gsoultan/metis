import { afterEach, describe, expect, test } from "bun:test";
import { auditService } from "./auditService";
import { stubFetch } from "../shared/stubbedFetch";

describe("auditService.listOperations", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("asks for the filters and cursor it was given", async () => {
    const stub = stubFetch({ operations: [], next: "c2" });
    restore = stub.restore;
    const page = await auditService.listOperations({ actor: "alice", type: "node_skipped" }, "c1");
    const url = new URL(stub.sent[0].url, "http://stub.invalid");
    expect(url.pathname).toEndWith("/audit/operations");
    expect(url.searchParams.get("actor")).toBe("alice");
    expect(url.searchParams.get("type")).toBe("node_skipped");
    expect(url.searchParams.get("before")).toBe("c1");
    expect(page.next).toBe("c2");
  });

  test("reads an omitted cursor and list as the last, empty page", async () => {
    ({ restore } = stubFetch({}));
    expect(await auditService.listOperations({})).toEqual({ operations: [], next: undefined });
  });

  test("raises the server's refusal", async () => {
    ({ restore } = stubFetch({ error: "forbidden: administrators only" }, 403));
    await expect(auditService.listOperations({})).rejects.toThrow("forbidden: administrators only");
  });
});
