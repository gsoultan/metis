import { API_BASE_URL } from "./config";
import { getAuthToken } from "./auth";
import { reportSessionRefused } from "./sessionRefusal";

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

const toErrorMessage = (statusText: string, fallback: string) => {
  if (statusText) {
    return statusText;
  }

  return fallback;
};

export const requestJSON = async <T>(path: string, options: RequestOptions = {}): Promise<T> => {
  const { method = "GET", body, signal, headers = {}, auth = true, checksCredentials = false } = options;

  const token = auth ? getAuthToken() : null;
  const finalHeaders: Record<string, string> = {
    ...headers,
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
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

  // Only a request that carried the session's token can say the session is
  // over: /login and /setup send none, and a wrong password there is a 401 too.
  if (response.status === 401 && token && !checksCredentials) {
    reportSessionRefused(token);
  }

  if (response.status === 204) {
    return {} as T;
  }

  const data = (await response.json().catch(() => ({}))) as Record<string, unknown>;
  if (!response.ok) {
    const errorMessage = typeof data.error === "string"
      ? data.error
      : toErrorMessage(response.statusText, "Request failed");
    throw new Error(errorMessage);
  }

  return data as T;
};
