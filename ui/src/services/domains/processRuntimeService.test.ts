import { afterEach, describe, expect, test } from "bun:test";
import { processRuntimeService } from "./processRuntimeService";
import { stubFetch } from "../shared/stubbedFetch";

describe("processRuntimeService.startProcess", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ error: "no deployed definition with key expense" }));
    await expect(processRuntimeService.startProcess("p-1", "expense")).rejects.toThrow(
      "no deployed definition with key expense",
    );
  });

  test("hands back the instance that was started", async () => {
    ({ restore } = stubFetch({ instanceId: "i-1" }));
    const { instance_id } = await processRuntimeService.startProcess("p-1", "expense");
    expect(instance_id).toBe("i-1");
  });
});

describe("processRuntimeService.waitingByStep", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("asks the server for the project's waiting work and returns its processes", async () => {
    const stub = stubFetch({ processes: [{ key: "quotation", name: "Quotation approval", instances: 2, steps: [{ node_id: "review", waiting: 2 }] }] });
    restore = stub.restore;
    const processes = await processRuntimeService.waitingByStep("p-1");
    expect(stub.sent[0].url).toContain("/projects/p-1/waiting");
    expect(processes).toHaveLength(1);
    expect(processes[0].steps?.[0]).toEqual({ node_id: "review", waiting: 2 });
  });

  test("raises a refusal rather than drawing an empty heat map", async () => {
    const stub = stubFetch({ err: "forbidden" });
    restore = stub.restore;
    await expect(processRuntimeService.waitingByStep("p-1")).rejects.toThrow("forbidden");
  });
});

describe("processRuntimeService.deadlines", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("asks the server for the project's open work with a deadline", async () => {
    const stub = stubFetch({
      deadlines: {
        tasks: [{ id: "t-1", name: "Review", due_date: "2020-01-01T00:00:00Z", process_name: "Quotation approval" }],
        with_deadline: 1,
        without_deadline: 205,
      },
    });
    restore = stub.restore;
    const deadlines = await processRuntimeService.deadlines("p-1");
    expect(stub.sent[0].url).toContain("/projects/p-1/deadlines");
    expect(deadlines.tasks?.[0].process_name).toBe("Quotation approval");
    expect(deadlines.without_deadline).toBe(205);
  });

  test("raises a refusal rather than reporting that nothing is late", async () => {
    const stub = stubFetch({ err: "forbidden" });
    restore = stub.restore;
    await expect(processRuntimeService.deadlines("p-1")).rejects.toThrow("forbidden");
  });
});
