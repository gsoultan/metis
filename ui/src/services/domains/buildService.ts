/**
 * Reading the running build off the liveness probe.
 *
 * `/healthz` is served outside `/api/v1`, by the outermost handler, ahead of
 * authentication — an orchestrator polling it has no credentials. That is also
 * why nothing here sends a token: a bearer on a probe buys nothing and puts a
 * session into one more access log.
 */
import { API_BASE_URL } from '../shared/config';

const HEALTH_PATH = '/healthz';

/**
 * Where the probe lives relative to the API.
 *
 * `API_BASE_URL` is `/api/v1` in every normal deployment, where the server
 * serves this bundle and the API from one origin, so the probe is a plain
 * absolute path. When it has been pointed at a different backend, the probe has
 * to follow it there rather than being read off whatever host is serving the
 * page — a leading-slash path resolves against that backend's origin, which
 * drops the `/api/v1` and lands on its `/healthz`.
 */
function healthUrl(): string {
  return API_BASE_URL.startsWith('http') ? new URL(HEALTH_PATH, API_BASE_URL).toString() : HEALTH_PATH;
}

export const buildService = {
  async getBuildVersion(signal?: AbortSignal): Promise<string> {
    const response = await fetch(healthUrl(), { signal, headers: { Accept: 'application/json' } });
    if (!response.ok) {
      throw new Error(`The liveness probe answered ${response.status}`);
    }
    const body = (await response.json()) as { version?: unknown };
    return typeof body.version === 'string' ? body.version : '';
  },
};
