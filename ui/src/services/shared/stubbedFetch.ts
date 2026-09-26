/**
 * A stand-in for `fetch` for service tests.
 *
 * Both transports end up in `globalThis.fetch` — requestJSON calls it directly
 * and the Connect transport resolves it per call — so one stub covers every
 * service method without mocking modules. Connect answers a unary call with
 * the response message as JSON, the same shape the HTTP endpoints use, which
 * is why a refusal can be staged the same way for both: `{ error }` for
 * Connect, `{ err }` for REST.
 */

/** What the service under test actually sent. */
export interface RecordedRequest {
  url: string;
  method: string;
  body: unknown;
}

/** Only the part of the global object the stub stands in for. */
type GlobalWithStorage = typeof globalThis & { localStorage?: Pick<Storage, "getItem"> };

/**
 * Answers every request with `body` and records what was asked. Returns a
 * function that puts the real `fetch` and `localStorage` back.
 */
export function stubFetch(body: unknown, status = 200): { sent: RecordedRequest[]; restore: () => void } {
  return stubFetchAnswering(() => body, status);
}

/**
 * stubFetch for something that makes more than one read: each request is
 * answered with whatever `answer` returns for the URL it asked for.
 */
export function stubFetchAnswering(
  answer: (url: URL) => unknown,
  status = 200,
): { sent: RecordedRequest[]; restore: () => void } {
  const sent: RecordedRequest[] = [];
  const global = globalThis as GlobalWithStorage;
  const originalFetch = globalThis.fetch;
  const originalStorage = global.localStorage;

  // A token, so the auth header code path runs rather than being skipped.
  global.localStorage = { getItem: () => JSON.stringify({ state: { token: "token-123" } }) } as unknown as Storage;
  globalThis.fetch = (async (input, init) => {
    sent.push({
      url: String(input),
      method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
    });
    return new Response(JSON.stringify(answer(new URL(String(input), "http://stub.invalid"))), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;

  return {
    sent,
    restore: () => {
      globalThis.fetch = originalFetch;
      global.localStorage = originalStorage;
    },
  };
}
