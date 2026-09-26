import { afterEach, describe, expect, test } from "bun:test";

import { API_BASE_URL } from "../shared/config";
import { stubFetch } from "../shared/stubbedFetch";
import { roleService } from "./roleService";

describe("roleService", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("asks the server what each role is required for", async () => {
    const stub = stubFetch({
      roles: [
        { role: "DESIGNER", actions: [{ method: "CreateDefinition", area: "processes", label: "Create definition" }] },
      ],
    });
    restore = stub.restore;

    const roles = await roleService.listRoles();

    expect(stub.sent[0].method).toBe("GET");
    expect(stub.sent[0].url).toBe(`${API_BASE_URL}/roles`);
    expect(roles).toEqual([
      { role: "DESIGNER", actions: [{ method: "CreateDefinition", area: "processes", label: "Create definition" }] },
    ]);
  });

  test("reads a role listed with no actions as an empty list, not a missing one", async () => {
    ({ restore } = stubFetch({ roles: [{ role: "QUERY_AUTHOR", actions: null }] }));
    expect(await roleService.listRoles()).toEqual([{ role: "QUERY_AUTHOR", actions: [] }]);
  });

  test("raises a refusal instead of answering with nothing", async () => {
    ({ restore } = stubFetch({ error: "unauthorized" }, 401));
    await expect(roleService.listRoles()).rejects.toThrow("unauthorized");
  });
});
