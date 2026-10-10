import { afterEach, describe, expect, test } from "bun:test";
import type { ProcessDefinition } from "../../gen/entities/definition_pb";
import { definitionService, fromDefinitionMessage } from "./definitionService";
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

// A process imported from a BPMN file keeps a sub-process's steps nested inside
// it. The loader read only the top level, so the designer opened the
// sub-process empty, the version comparison could not see inside it, and
// saving from the designer dropped the steps it had never shown.
describe("fromDefinitionMessage", () => {
  const imported = {
    id: "d-1",
    key: "onboard",
    name: "Onboard",
    version: 1,
    nodes: [
      { id: "start", type: "startEvent", nodes: [], flows: [] },
      {
        id: "checks",
        type: "subProcess",
        width: 420,
        height: 180,
        isExpanded: true,
        nodes: [
          { id: "checksStart", type: "startEvent", nodes: [], flows: [] },
          { id: "verifyId", type: "userTask", name: "Verify the ID", nodes: [], flows: [] },
          { id: "checksEnd", type: "endEvent", nodes: [], flows: [] },
        ],
        flows: [
          { id: "c1", sourceRef: "checksStart", targetRef: "verifyId" },
          { id: "c2", sourceRef: "verifyId", targetRef: "checksEnd" },
        ],
      },
      { id: "caught", type: "boundaryEvent", attachedToRef: "checks", errorCode: "ID_REJECTED", nodes: [], flows: [] },
      { id: "end", type: "endEvent", nodes: [], flows: [] },
    ],
    flows: [
      { id: "f1", sourceRef: "start", targetRef: "checks" },
      { id: "f2", sourceRef: "checks", targetRef: "end" },
    ],
  } as unknown as ProcessDefinition;

  test("keeps the steps inside a sub-process, with the sub-process as their parent", () => {
    const definition = fromDefinitionMessage(imported);
    const inside = (definition?.nodes ?? []).filter((n) => n.parent_id === "checks").map((n) => n.id);
    expect(inside).toEqual(["checksStart", "verifyId", "checksEnd"]);
  });

  test("keeps the paths inside a sub-process", () => {
    const flows = (fromDefinitionMessage(imported)?.flows ?? []).map((f) => f.id);
    expect(flows).toEqual(["f1", "f2", "c1", "c2"]);
  });

  test("keeps a diagram's sizes and an error boundary's code", () => {
    const nodes = fromDefinitionMessage(imported)?.nodes ?? [];
    const checks = nodes.find((n) => n.id === "checks");
    expect([checks?.width, checks?.height, checks?.is_expanded]).toEqual([420, 180, true]);
    expect(nodes.find((n) => n.id === "caught")?.error_code).toBe("ID_REJECTED");
  });
});

// The migrate route answers an apply in three ways that are not failures, and
// the service handed on only `plan` and `applied`: a request sent to a second
// administrator read as "applied: false" and nothing else, and the instances a
// run passed over were dropped before anything could show them.
describe("definitionService.migrateInstances", () => {
  let restore = () => {};
  afterEach(() => restore());

  const plan = { source_key: "quotation", source_version: 2, target_version: 5, target_id: "d-5", instances: 3, requires_second_approver: true };
  const pending = {
    request_id: "0199c0de-0000-7000-8000-00000000aaaa",
    status: "pending_approval",
    requested_by: "Dita Larasati",
    expires_at: "2026-10-06T09:12:00Z",
    because: ["“Operations approve” would be skipped"],
  };

  test("reads an apply sent for approval — a 202 — as an answer, with the request it waits on", async () => {
    const stub = stubFetch({ plan, applied: false, passed_over: [], passed_over_in_all: 0, pending_approval: pending }, 202);
    restore = stub.restore;
    const reply = await definitionService.migrateInstances("d-2", "d-5", {}, false);
    expect(stub.sent[0].body).toMatchObject({ source_definition_id: "d-2", target_definition_id: "d-5", dry_run: false });
    expect(reply.err).toBeUndefined();
    expect(reply.applied).toBe(false);
    expect(reply.pending_approval).toEqual(pending);
    expect(reply.plan).toEqual(plan);
    expect(reply.passed_over).toEqual([]);
    expect(reply.passed_over_in_all).toBe(0);
  });

  test("hands on the instances a run passed over, and how many there were", async () => {
    const passed = [{
      instance_id: "0199c0de-0000-7000-8000-000000000001",
      cause: "left_the_step",
      steps: [{ node_id: "opsApprove", name: "Operations approve" }],
      steps_in_all: 1,
      reason: "It was no longer waiting there.",
    }];
    const stub = stubFetch({ plan, applied: true, passed_over: passed, passed_over_in_all: 340 });
    restore = stub.restore;
    const reply = await definitionService.migrateInstances("d-2", "d-5", {}, false);
    expect(reply.applied).toBe(true);
    expect(reply.pending_approval).toBeUndefined();
    expect(reply.passed_over).toEqual(passed);
    expect(reply.passed_over_in_all).toBe(340);
  });

  test("reads a reply from a server that says nothing of either as nobody passed over and nobody asked", async () => {
    const stub = stubFetch({ plan, applied: true });
    restore = stub.restore;
    const reply = await definitionService.migrateInstances("d-2", "d-5", {}, false);
    expect(reply.passed_over).toEqual([]);
    expect(reply.passed_over_in_all).toBe(0);
    expect(reply.pending_approval).toBeUndefined();
  });

  test("still raises a refusal: a 400 or a 403 is not an answer", async () => {
    ({ restore } = stubFetch({ error: "forbidden: only an administrator may migrate running instances" }, 403));
    await expect(definitionService.migrateInstances("d-2", "d-5", {}, false)).rejects.toThrow(
      "forbidden: only an administrator may migrate running instances",
    );
  });
});
