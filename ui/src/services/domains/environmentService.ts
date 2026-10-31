import { requestJSON } from "../shared/rest";
import type {
  ApiEnvironment,
  DeleteEnvironmentResponse,
  ListEnvironmentsResponse,
  SaveEnvironmentResponse,
  TestEnvironmentConnectionResponse,
} from "../types";
import { raiseIfRefused } from "../raise";

/**
 * The runtimes a project deploys into.
 *
 * Every operation here is administrative: an environment names a database and
 * the credentials to reach it, so even reading the list says where each runtime
 * lives. The server enforces that; this only calls it.
 */
export const environmentService = {
  async listEnvironments(projectId: string, signal?: AbortSignal) {
    const response = await requestJSON<ListEnvironmentsResponse>(
      `/environments?project_id=${encodeURIComponent(projectId)}`,
      { method: "GET", signal },
    );
    return { environments: response.environments ?? [], err: response.err };
  },

  /**
   * Creates an environment, or updates one when the draft carries an id.
   *
   * A password left as the masking sentinel is sent back unchanged, which the
   * server reads as "keep the stored one" — so editing a host does not require
   * re-typing a credential the browser was never given.
   */
  async saveEnvironment(projectId: string, environment: Partial<ApiEnvironment>, signal?: AbortSignal) {
    const response = await requestJSON<SaveEnvironmentResponse>("/environments", {
      method: "POST",
      body: {
        id: environment.id,
        project_id: projectId,
        name: environment.name,
        port: environment.port,
        driver: environment.driver,
        connection: environment.connection,
        enabled: environment.enabled,
        protected: environment.protected,
      },
      signal,
    });
    return { id: raiseIfRefused(response).id };
  },

  /**
   * Opens the described database and reports whether it answered.
   *
   * A password left as the masking placeholder is resolved server-side against
   * the stored one, so an existing environment can be tested without re-typing
   * a credential this browser was never given.
   */
  async testEnvironmentConnection(
    environment: { id?: string; driver?: string; connection?: Record<string, unknown> },
    signal?: AbortSignal,
  ) {
    const response = await requestJSON<TestEnvironmentConnectionResponse>("/environments/test-connection", {
      method: "POST",
      body: { id: environment.id, driver: environment.driver, connection: environment.connection },
      signal,
    });
    const accepted = raiseIfRefused(response);
    return { reachable: accepted.reachable ?? false, detail: accepted.detail ?? "" };
  },

  /**
   * Removes an environment from the registry.
   *
   * The database it named is left alone — the runtime stops being served, which
   * is not the same decision as destroying what it ran.
   */
  async deleteEnvironment(id: string, signal?: AbortSignal) {
    const response = await requestJSON<DeleteEnvironmentResponse>(
      `/environments/${encodeURIComponent(id)}`,
      { method: "DELETE", signal },
    );
    return { err: raiseIfRefused(response).err };
  },
};
