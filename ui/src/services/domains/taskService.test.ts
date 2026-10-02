import { afterEach, describe, expect, test } from "bun:test";
import { taskService } from "./taskService";
import { stubFetch } from "../shared/stubbedFetch";

/*
 * The server takes a hand-over from anyone but the task's holder only with a
 * reason. None of these calls could carry one, so an administrator's reassign,
 * edit or release of somebody else's task could only be refused.
 */
describe("taskService hand-overs", () => {
  let restore = () => {};
  afterEach(() => restore());

  const sending = () => {
    const stub = stubFetch({});
    restore = stub.restore;
    return stub;
  };

  test("assignTask carries the reason when there is one, and no empty one when there is not", async () => {
    const stub = sending();
    await taskService.assignTask("t1", "citra", "Mallory is on leave");
    await taskService.assignTask("t1", "citra");
    expect(stub.sent[0].method).toBe("POST");
    expect(stub.sent[0].url).toEndWith("/tasks/t1/assign");
    expect(stub.sent[0].body).toEqual({ user_id: "citra", reason: "Mallory is on leave" });
    expect(stub.sent[1].body).toEqual({ user_id: "citra" });
  });

  test("delegateTask carries the reason when there is one", async () => {
    const stub = sending();
    await taskService.delegateTask("t1", "citra", "Covering for budi");
    await taskService.delegateTask("t1", "citra");
    expect(stub.sent[0].url).toEndWith("/tasks/t1/delegate");
    expect(stub.sent[0].body).toEqual({ user_id: "citra", reason: "Covering for budi" });
    expect(stub.sent[1].body).toEqual({ user_id: "citra" });
  });

  test("updateTask carries the reason beside the three fields it always sends", async () => {
    const stub = sending();
    await taskService.updateTask("t1", "Approve the refund", 50, "", "The deadline moved");
    await taskService.updateTask("t1", "Approve the refund", 50, "");
    expect(stub.sent[0].method).toBe("PUT");
    expect(stub.sent[0].url).toEndWith("/tasks/t1");
    expect(stub.sent[0].body).toEqual({ name: "Approve the refund", priority: 50, due_date: "", reason: "The deadline moved" });
    expect(stub.sent[1].body).toEqual({ name: "Approve the refund", priority: 50, due_date: "" });
  });

  test("releaseTaskFor releases somebody else's task over the route that takes a reason", async () => {
    const stub = sending();
    await taskService.releaseTaskFor("t1", "Mallory has left the team");
    expect(stub.sent[0].method).toBe("POST");
    expect(stub.sent[0].url).toEndWith("/tasks/t1/unclaim");
    expect(stub.sent[0].body).toEqual({ reason: "Mallory has left the team" });
  });

  test("resolveTask hands a task back, with a reason only when one is given", async () => {
    const stub = sending();
    await taskService.resolveTask("t1");
    await taskService.resolveTask("t1", "Mallory is away and budi needs it today");
    expect(stub.sent[0].method).toBe("POST");
    expect(stub.sent[0].url).toEndWith("/tasks/t1/resolve");
    expect(stub.sent[0].body).toEqual({});
    expect(stub.sent[1].body).toEqual({ reason: "Mallory is away and budi needs it today" });
  });

  /*
   * A refusal is a sentence written for the person who was refused. It reaches
   * them as it is, whether the server answered 200 with it in the body or
   * refused the request outright.
   */
  test("a refusal is raised in the server's own words", async () => {
    const refusal = "say why you are reassigning it: a reason is required from anyone but the person holding the task";
    ({ restore } = stubFetch({ error: refusal }, 400));
    await expect(taskService.assignTask("t1", "citra")).rejects.toThrow(refusal);
    restore();
    ({ restore } = stubFetch({ error: "only the person this task was delegated to, or an administrator, can hand it back" }, 403));
    await expect(taskService.resolveTask("t1")).rejects.toThrow("only the person this task was delegated to, or an administrator, can hand it back");
    restore();
    ({ restore } = stubFetch({ err: "this task has not been delegated, so there is nothing to hand back" }));
    await expect(taskService.resolveTask("t1")).rejects.toThrow("this task has not been delegated, so there is nothing to hand back");
    await expect(taskService.releaseTaskFor("t1", "why")).rejects.toThrow("nothing to hand back");
  });
});

describe("taskService.listTasksDelegatedByMe", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("reads one page of what the signed-in person delegated, naming nobody", async () => {
    const stub = stubFetch({
      tasks: [{ id: "t3", name: "Sign the contract", assignee: { username: "citra" }, owner: { username: "mallory" } }],
      page: { total: 31, page: 1, page_size: 25, has_more: true },
    });
    restore = stub.restore;
    const result = await taskService.listTasksDelegatedByMe({ page: 1, pageSize: 25 });
    expect(stub.sent[0].method).toBe("GET");
    expect(stub.sent[0].url).toEndWith("/tasks/delegated?page=1&page_size=25");
    expect(result.tasks.map((task) => `${task.name} / ${task.assignee?.username}`)).toEqual(["Sign the contract / citra"]);
    // How many there are, not how many came back.
    expect(result.total).toBe(31);
  });

  test("is an empty list when nothing is delegated", async () => {
    ({ restore } = stubFetch({}));
    expect(await taskService.listTasksDelegatedByMe()).toEqual({ tasks: [], total: 0 });
  });

  test("raises a refusal rather than reading as nothing delegated", async () => {
    ({ restore } = stubFetch({ err: "sign in to see what you delegated" }));
    await expect(taskService.listTasksDelegatedByMe()).rejects.toThrow("sign in to see what you delegated");
  });
});
