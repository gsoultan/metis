import { afterEach, describe, expect, test } from "bun:test";
import { identityService } from "./identityService";
import { stubFetch } from "../shared/stubbedFetch";

const newUser = {
  organization_id: "org-1",
  username: "dana",
  password: "correct horse battery",
  full_name: "Dana Scully",
  display_name: "Dana",
  organization: "",
  email: "dana@example.com",
  roles: ["USER"],
};

describe("identityService writes", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("createUser raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ err: "username already taken" }));
    await expect(identityService.createUser(newUser)).rejects.toThrow("username already taken");
  });

  test("createUser sends the organization as a membership the server reads", async () => {
    const stub = stubFetch({ user: { id: "u-1" } });
    restore = stub.restore;
    await identityService.createUser(newUser);

    const body = stub.sent[0].body as { user: Record<string, unknown>; password: string };
    expect(body.user.organizations).toEqual([{ id: "org-1" }]);
    // Neither is read by the server; the free-text one broke the decode.
    expect(body.user).not.toHaveProperty("organization_id");
    expect(body.user).not.toHaveProperty("organization");
    expect(body.password).toBe("correct horse battery");
  });

  test("updateUser omits roles when the caller did not set them", async () => {
    const stub = stubFetch({});
    restore = stub.restore;
    await identityService.updateUser("u-1", { full_name: "Dana Scully", display_name: "Dana", email: "dana@example.com" });

    const body = stub.sent[0].body as { user: Record<string, unknown> };
    expect(body.user).not.toHaveProperty("roles");
    expect(body.user).not.toHaveProperty("organization");
    expect(body.user.id).toBe("u-1");
  });

  test("createGroup raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ err: "group name in use" }));
    await expect(
      identityService.createGroup({ organization_id: "org-1", name: "Approvers", description: "", roles: ["USER"] }),
    ).rejects.toThrow("group name in use");
  });

  test("updateOwnProfile saves to the caller's own profile, not to an account named by id", async () => {
    // PUT /users/{id} is an administrator's, so the Profile page's save failed
    // for everybody else.
    const stub = stubFetch({});
    restore = stub.restore;
    await identityService.updateOwnProfile({ full_name: "Dana Scully", display_name: "Dana", email: "dana@example.com" });

    expect(stub.sent[0].method).toBe("PUT");
    expect(stub.sent[0].url.endsWith("/users/me")).toBe(true);
    expect((stub.sent[0].body as { user: unknown }).user).toEqual({
      full_name: "Dana Scully",
      display_name: "Dana",
      email: "dana@example.com",
    });
  });

  test("updateOwnProfile sends the three fields and nothing else it is handed", async () => {
    const stub = stubFetch({});
    restore = stub.restore;
    const handed = { full_name: "Dana", display_name: "Dana", email: "", roles: ["ADMIN"], organization: "Other", id: "u-2" };
    await identityService.updateOwnProfile(handed);

    const user = (stub.sent[0].body as { user: Record<string, unknown> }).user;
    expect(Object.keys(user).sort()).toEqual(["display_name", "email", "full_name"]);
  });

  test("updateOwnProfile raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ error: "invalid argument: the email has to be a single address" }, 400));
    await expect(
      identityService.updateOwnProfile({ full_name: "Dana", display_name: "Dana", email: "dana at example" }),
    ).rejects.toThrow("single address");
  });

  test("getOwnProfile reads the caller's own profile", async () => {
    const stub = stubFetch({ user: { id: "u-1", username: "dana", email: "dana@example.com" } });
    restore = stub.restore;
    const { user } = await identityService.getOwnProfile();

    expect(stub.sent[0].method).toBe("GET");
    expect(stub.sent[0].url.endsWith("/users/me")).toBe(true);
    expect(user?.email).toBe("dana@example.com");
  });

  test("createGroup and updateGroup send the group's roles", async () => {
    const stub = stubFetch({ group: { id: "g-1" } });
    restore = stub.restore;
    await identityService.createGroup({ organization_id: "org-1", name: "Approvers", description: "", roles: ["OPERATOR"] });
    await identityService.updateGroup("g-1", { name: "Approvers", description: "", roles: ["OPERATOR", "USER"] });

    expect((stub.sent[0].body as { group: { roles: string[] } }).group.roles).toEqual(["OPERATOR"]);
    expect((stub.sent[1].body as { group: { roles: string[] } }).group.roles).toEqual(["OPERATOR", "USER"]);
  });
});
