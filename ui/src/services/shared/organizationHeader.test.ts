import { afterEach, describe, expect, test } from "bun:test";
import { getAuthHeaders, ORGANIZATION_HEADER } from "./auth";
import { organizationClient, projectClient } from "./connect";
import { requestJSON } from "./rest";

/**
 * Every request says which organization it is for.
 *
 * The server works in the organization a request names and, when it names
 * none, in the caller's first. The app named none, so choosing another
 * organization in the header changed the page and nothing the server did.
 */

type GlobalWithStorage = typeof globalThis & { localStorage?: Pick<Storage, "getItem"> };
const global = globalThis as GlobalWithStorage;
const originalFetch = globalThis.fetch;
const originalStorage = global.localStorage;

/** Signs in as somebody working in `organization`, and records the headers of every request. */
function workingIn(organization: string | null, token: string | null = "token-123"): Headers[] {
  const sent: Headers[] = [];
  global.localStorage = {
    getItem: () => JSON.stringify({ state: { token, currentOrganizationId: organization } }),
  } as unknown as Storage;
  globalThis.fetch = (async (_input, init) => {
    sent.push(new Headers(init?.headers));
    return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
  }) as typeof fetch;
  return sent;
}

afterEach(() => {
  globalThis.fetch = originalFetch;
  global.localStorage = originalStorage;
});

describe("the organization a request is for", () => {
  test("a REST request names the organization being worked in", async () => {
    const sent = workingIn("org-b");
    await requestJSON("/anything");
    expect(sent[0].get(ORGANIZATION_HEADER)).toBe("org-b");
    expect(sent[0].get("Authorization")).toBe("Bearer token-123");
  });

  test("a Connect call names the organization being worked in", async () => {
    const sent = workingIn("org-b");
    await projectClient.listProjects({});
    expect(sent[0].get(ORGANIZATION_HEADER)).toBe("org-b");
  });

  test("the list of organizations names none, so a stale choice can be corrected", async () => {
    // An organization the account has left is refused for every request that
    // names it; the list that replaces the choice must still be answered.
    const sent = workingIn("org-left-behind");
    await organizationClient.listOrganizations({});
    expect(sent[0].get(ORGANIZATION_HEADER)).toBeNull();
    expect(sent[0].get("Authorization")).toBe("Bearer token-123");
  });

  test("with none chosen, no organization is named and the server picks the first", async () => {
    const sent = workingIn(null);
    await requestJSON("/anything");
    await projectClient.listProjects({});
    expect(sent.map((headers) => headers.get(ORGANIZATION_HEADER))).toEqual([null, null]);
  });

  test("signed out, nothing is sent at all", () => {
    workingIn("org-b", null);
    expect(getAuthHeaders()).toEqual({});
  });
});
