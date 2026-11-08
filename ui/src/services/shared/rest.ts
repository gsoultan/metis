import { API_BASE_URL } from "./config";
import { getAuthHeaders, getAuthToken } from "./auth";
import { reportSessionRefused } from "./sessionRefusal";
import { statusMessage } from "./errors";

type RequestOptions = {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
  auth?: boolean;
  /**
   * The request checks credentials of its own — the current password when
   * changing it — so a 401 means those were wrong, not that the session ended.
   */
  checksCredentials?: boolean;
};

/** A reply the server refused, with the status it answered. */
export class RequestError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "RequestError";
    this.status = status;
  }
}

export const requestJSON = async <T>(path: string, options: RequestOptions = {}): Promise<T> => {
  const { method = "GET", body, signal, headers = {}, auth = true, checksCredentials = false } = options;

  const token = auth ? getAuthToken() : null;
  const finalHeaders: Record<string, string> = {
    ...headers,
    ...(token ? getAuthHeaders() : {}),
  };

  if (body !== undefined && !finalHeaders["Content-Type"]) {
    finalHeaders["Content-Type"] = "application/json";
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    method,
    headers: finalHeaders,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });

  // Only a request that asked for the session can say the session is over:
  // /login and /setup do not (auth: false), and a wrong password there is a
  // 401 too. One that asked and found no token says the token was taken out
  // from under this tab — see sessionRefusal.
  if (response.status === 401 && auth && !checksCredentials) {
    reportSessionRefused(token);
  }

  if (response.status === 204) {
    return {} as T;
  }

  const data = (await response.json().catch(() => ({}))) as Record<string, unknown>;
  if (!response.ok) {
    // A reply with no words of its own — http.Error's plain-text "Unauthorized",
    // a proxy's HTML error page — is described by its status, in plain words,
    // not by the status line ("Unauthorized", "Request failed").
    const errorMessage = typeof data.error === "string" && data.error.trim() !== ""
      ? data.error
      : statusMessage(response.status);
    throw new RequestError(errorMessage, response.status);
  }

  return data as T;
};
