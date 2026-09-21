/**
 * The sandbox's own client, deliberately separate from the rest of this app.
 *
 * Every other screen talks to the server over Connect, where a protobuf schema
 * gives both ends a checked contract. This one goes over the public REST API —
 * the surface the Go SDK and every other integrator speaks — because a sandbox
 * that exercised the private transport would prove nothing about theirs. If a
 * REST body or a path is wrong, this screen is where it shows.
 *
 * It also keeps the whole exchange: status, latency, both bodies. `requestJSON`
 * throws the status away and returns only the parsed body, which is exactly the
 * information somebody debugging an integration came here for.
 *
 * **A refusal is a result, not an exception.** A 409 on a lock somebody else
 * holds is the server working correctly, and the caller wants to read it rather
 * than catch it. Only an aborted request throws, so React Query can cancel.
 */
import { requestFor, describeCall, type SdkCall } from '../../domain/sdkCalls';
import type { WireCall } from '../../domain/sdkWireLog';
import { getAuthHeaders } from '../shared/auth';
import { API_BASE_URL } from '../shared/config';

export interface SdkCallOutcome {
  /** What to show in the wire log, whatever happened. */
  record: WireCall;
  /** The parsed reply, present whenever the server sent JSON. */
  body: Record<string, unknown> | null;
  ok: boolean;
}

/** Where a snippet should point so it runs unedited. */
export function snippetBaseUrl(): string {
  if (API_BASE_URL.startsWith('http')) {
    return API_BASE_URL;
  }
  // API_BASE_URL is relative in every normal deployment, because the server
  // serves this bundle and the API from one origin. A snippet has to be
  // absolute to be runnable, so it is resolved against wherever this page is.
  return `${window.location.origin}${API_BASE_URL}`;
}

export async function executeSdkCall(call: SdkCall, signal?: AbortSignal): Promise<SdkCallOutcome> {
  const request = requestFor(call);
  const started = performance.now();

  const record: WireCall = {
    id: crypto.randomUUID(),
    label: describeCall(call),
    method: request.method,
    path: request.path,
    status: null,
    durationMs: 0,
    requestBody: request.body,
    at: Date.now(),
  };

  try {
    const response = await fetch(`${API_BASE_URL}${request.path}`, {
      method: request.method,
      headers: {
        ...(request.body === undefined ? {} : { 'Content-Type': 'application/json' }),
        ...request.headers,
        // Last, and never recorded: the wire log is the thing people paste into
        // issues and screenshot, and a bearer token in it is a leaked session.
        ...getAuthHeaders(),
      },
      body: request.body === undefined ? undefined : JSON.stringify(request.body),
      signal,
    });

    const body = (await response.json().catch(() => null)) as Record<string, unknown> | null;
    record.status = response.status;
    record.durationMs = performance.now() - started;
    record.responseBody = body ?? undefined;

    // Two shapes carry a refusal: an error status with `{"error": …}`, and a
    // 200 whose body names `err`. The second is how several endpoints report a
    // rejected command, and reading only the status calls those a success.
    const refusal = refusalIn(response, body);
    if (refusal !== null) {
      record.error = refusal;
      return { record, body, ok: false };
    }

    return { record, body, ok: true };
  } catch (error) {
    if (signal?.aborted) {
      throw error;
    }
    record.durationMs = performance.now() - started;
    record.error = unreachable(error);
    return { record, body: null, ok: false };
  }
}

function refusalIn(response: Response, body: Record<string, unknown> | null): string | null {
  const reported = typeof body?.error === 'string' ? body.error : typeof body?.err === 'string' ? body.err : '';
  if (!response.ok) {
    return reported !== '' ? reported : `The server answered ${response.status} ${response.statusText}`.trim();
  }
  return reported !== '' ? reported : null;
}

/**
 * A network failure, said in a way that names the likely cause.
 *
 * `fetch` rejects with "Failed to fetch" for a server that is down, a DNS
 * failure, a CORS refusal and an offline laptop alike — four different fixes
 * behind one sentence, so the sentence has to open the question rather than
 * pretend to answer it.
 */
function unreachable(error: unknown): string {
  const detail = error instanceof Error ? error.message : String(error);
  return `The request never reached Metis (${detail}). Check the server is running and reachable from this browser.`;
}

/** The instance as the REST API reports it — the same read `client.GetInstance` does. */
export interface SandboxInstance {
  id: string;
  status: string;
  variables: Record<string, unknown>;
  definitionName?: string;
}

export async function readInstance(instanceId: string, signal?: AbortSignal): Promise<SandboxInstance | null> {
  const response = await fetch(`${API_BASE_URL}/instances/${instanceId}`, {
    headers: getAuthHeaders(),
    signal,
  });
  if (!response.ok) {
    return null;
  }
  const body = (await response.json().catch(() => null)) as {
    instance?: {
      id?: string;
      status?: string;
      variables?: Record<string, unknown>;
      definition?: { name?: string };
    };
  } | null;
  const instance = body?.instance;
  if (!instance?.id) {
    return null;
  }
  return {
    id: instance.id,
    status: instance.status ?? 'unknown',
    variables: instance.variables ?? {},
    definitionName: instance.definition?.name,
  };
}
