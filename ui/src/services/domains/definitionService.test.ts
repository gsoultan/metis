import { afterEach, describe, expect, test } from "bun:test";
import { definitionService } from "./definitionService";
import { stubFetch } from "../shared/stubbedFetch";

describe("definitionService.createDefinition", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ error: "definition key already deployed" }));
    await expect(
      definitionService.createDefinition("p-1", { key: "expense", name: "Expense", nodes: [], flows: [] }),
    ).rejects.toThrow("definition key already deployed");
  });
});

describe("definitionService.importDefinition", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("says which project the model goes into, which the endpoint requires", async () => {
    const stub = stubFetch({ id: "d-1" });
    restore = stub.restore;
    await definitionService.importDefinition("p-1", "<definitions/>");
    expect(stub.sent[0].body).toMatchObject({ project_id: "p-1" });
  });

  test("sends a model whose names are not Latin-1, which btoa refused", async () => {
    const stub = stubFetch({ id: "d-1" });
    restore = stub.restore;
    await definitionService.importDefinition("p-1", '<task name="审批"/>');
    const sent = stub.sent[0].body as { xml: string };
    expect(new TextDecoder().decode(Uint8Array.from(atob(sent.xml), (c) => c.charCodeAt(0)))).toBe('<task name="审批"/>');
  });
});
