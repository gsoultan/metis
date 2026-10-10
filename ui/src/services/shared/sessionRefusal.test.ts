import { afterEach, describe, expect, jest, test } from "bun:test";
import { createStore } from "zustand/vanilla";

import { requestJSON } from "./rest";
import { organizationClient } from "./connect";
import { endSessionOnRefusal, onSessionRefused } from "./sessionRefusal";
import { stubFetch } from "./stubbedFetch";
import { subscribeToEvents, ALL_EVENTS } from "../../hooks/useEventStream";

/** The token stubFetch puts in storage. */
const TOKEN = "token-123";

function signedIn() {
  return createStore<{ token: string | null; clearAuth: () => void }>()((set) => ({
    token: TOKEN,
    clearAuth: () => set({ token: null }),
  }));
}

async function settle() {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

describe("a token the server no longer accepts", () => {
  let restore = () => {};
  let stopListening = () => {};
  afterEach(() => {
    restore();
    stopListening();
  });

  function listen() {
    const store = signedIn();
    let sentToSignIn = 0;
    stopListening = onSessionRefused(endSessionOnRefusal(store, () => { sentToSignIn += 1; }));
    return { store, sentToSignIn: () => sentToSignIn };
  }

  test("ends the session and goes to the sign-in page once, however many requests are refused", async () => {
    const { store, sentToSignIn } = listen();
    ({ restore } = stubFetch({ error: "Unauthorized" }, 401));

    await Promise.allSettled([requestJSON("/tasks"), requestJSON("/instances"), requestJSON("/stats")]);

    expect(store.getState().token).toBeNull();
    expect(sentToSignIn()).toBe(1);
  });

  test("is heard from a Connect call too", async () => {
    const { store, sentToSignIn } = listen();
    ({ restore } = stubFetch({ code: "unauthenticated", message: "token expired" }, 401));

    await expect(organizationClient.listOrganizations({})).rejects.toThrow();

    expect(store.getState().token).toBeNull();
    expect(sentToSignIn()).toBe(1);
  });

  test("is not a wrong password at sign-in, which sends no token", async () => {
    const { store, sentToSignIn } = listen();
    ({ restore } = stubFetch({ error: "invalid credentials" }, 401));

    await expect(requestJSON("/login", { method: "POST", body: {}, auth: false })).rejects.toThrow();

    expect(store.getState().token).toBe(TOKEN);
    expect(sentToSignIn()).toBe(0);
  });

  test("is not a wrong current password when changing it", async () => {
    const { store, sentToSignIn } = listen();
    ({ restore } = stubFetch({ error: "invalid credentials" }, 401));

    await expect(requestJSON("/users/me/password", { method: "POST", body: {}, checksCredentials: true })).rejects.toThrow();

    expect(store.getState().token).toBe(TOKEN);
    expect(sentToSignIn()).toBe(0);
  });

  test("does not sign out a newer session when a request from the old one comes back late", async () => {
    const store = signedIn();
    store.setState({ token: "the-new-sign-in" });
    let sentToSignIn = 0;
    stopListening = onSessionRefused(endSessionOnRefusal(store, () => { sentToSignIn += 1; }));
    ({ restore } = stubFetch({ error: "Unauthorized" }, 401));

    await expect(requestJSON("/tasks")).rejects.toThrow();

    expect(store.getState().token).toBe("the-new-sign-in");
    expect(sentToSignIn).toBe(0);
  });

  test("stops the event stream reconnecting", async () => {
    jest.useFakeTimers();
    const { store } = listen();
    const stub = stubFetch({}, 401);
    restore = stub.restore;

    const unsubscribe = subscribeToEvents(ALL_EVENTS, () => {});
    await settle();
    jest.advanceTimersByTime(120_000);
    await settle();
    unsubscribe();
    jest.useRealTimers();

    expect(store.getState().token).toBeNull();
    expect(stub.sent.length).toBe(1);
  });
});
