import { requestJSON } from "../shared/rest";
import type {
  ApiConnector,
  ApiConnectorInstance,
  CreateConnectorInstancePayload,
  CreateConnectorPayload,
  TryConnectorStepRequest,
} from "../types";
import { raiseIfRefused } from "../raise";

type ConnectorListResponse = {
  connectors?: ApiConnector[];
  err?: string;
};

type ConnectorInstancesResponse = {
  instances?: ApiConnectorInstance[];
  err?: string;
};

type ConnectorInstanceResponse = {
  instance?: ApiConnectorInstance;
  err?: string;
};

type ConnectorResultResponse = {
  result?: Record<string, unknown>;
  variables?: Record<string, unknown>;
  err?: string;
};

type ManifestListResponse = { manifests?: ApiConnectorManifest[]; err?: string };

export const connectorService = {
  async listConnectors(signal?: AbortSignal) {
    const data = await requestJSON<ConnectorListResponse>("/connectors", { signal });
    return { connectors: data.connectors ?? [], err: data.err };
  },

  async createConnector(connector: CreateConnectorPayload, signal?: AbortSignal) {
    const data = await requestJSON<{ connector?: ApiConnector; err?: string }>("/connectors", {
      method: "POST",
      body: { connector },
      signal,
    });
    return { connector: raiseIfRefused(data).connector, err: data.err };
  },

  async updateConnector(connector: ApiConnector, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/${connector.id}`, {
      method: "PUT",
      body: { connector },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async deleteConnector(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/${id}`, {
      method: "DELETE",
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async listConnectorInstances(projectId: string, signal?: AbortSignal) {
    const data = await requestJSON<ConnectorInstancesResponse>(`/connectors/instances?project_id=${projectId}`, { signal });
    return { instances: data.instances ?? [], err: data.err };
  },

  async createConnectorInstance(instance: CreateConnectorInstancePayload, signal?: AbortSignal) {
    const data = await requestJSON<ConnectorInstanceResponse>("/connectors/instances", {
      method: "POST",
      body: { instance },
      signal,
    });
    return { instance: raiseIfRefused(data).instance, err: data.err };
  },

  async updateConnectorInstance(instance: ApiConnectorInstance, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/instances/${instance.id}`, {
      method: "PUT",
      body: { instance },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async deleteConnectorInstance(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/instances/${id}`, {
      method: "DELETE",
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async executeConnector(
    connectorKey: string,
    config: Record<string, unknown>,
    payload: Record<string, unknown>,
    signal?: AbortSignal,
  ) {
    const data = await requestJSON<ConnectorResultResponse>("/connectors/execute", {
      method: "POST",
      body: { connector_key: connectorKey, config, payload },
      signal,
    });
    return raiseIfRefused(data).result;
  },

  /**
   * Runs one connector step once, against the connection its project saved,
   * and returns what the step would store. It is real: whatever the step sends
   * is sent. Nothing is recorded on any process.
   */
  async tryConnectorStep(request: TryConnectorStepRequest, signal?: AbortSignal) {
    const data = await requestJSON<ConnectorResultResponse>("/connectors/try-step", {
      method: "POST",
      body: {
        project_id: request.projectId,
        step_name: request.stepName,
        properties: request.properties,
        variables: request.variables,
      },
      signal,
    });
    return raiseIfRefused(data).variables ?? {};
  },

  async executeScript(
    script: string,
    scriptFormat: string,
    variables: Record<string, unknown>,
    signal?: AbortSignal,
  ) {
    const data = await requestJSON<ConnectorResultResponse>("/processes/execute-script", {
      method: "POST",
      body: { script, script_format: scriptFormat, variables },
      signal,
    });
    return raiseIfRefused(data).variables;
  },

  /** The connectors installed as documents rather than compiled in. */
  async listConnectorManifests(signal?: AbortSignal) {
    const data = await requestJSON<ManifestListResponse>("/connector-manifests", { signal });
    return data.manifests ?? [];
  },

  /**
   * Installs one.
   *
   * `format` is "manifest" or "openapi" — one endpoint for both, because what a
   * person has in front of them is "a file the vendor published" and being asked
   * which upload button it belongs to is a question about our implementation.
   *
   * The body is passed as an object: `requestJSON` serialises it. Pre-serialising
   * here sent the server a JSON string literal, which its struct decoder
   * refuses, and the refusal arrived as `err` — which was then dropped, so the
   * screen reported "0 connectors installed" in green.
   */
  async installConnectorManifest(document: string, format: "manifest" | "openapi", signal?: AbortSignal) {
    const data = await requestJSON<ManifestListResponse>("/connector-manifests", {
      method: "POST",
      body: { document, format },
      signal,
    });
    return raiseIfRefused(data).manifests ?? [];
  },

  async setConnectorManifestEnabled(id: string, enabled: boolean, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connector-manifests/${id}/enabled`, {
      method: "POST",
      body: { enabled },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async deleteConnectorManifest(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connector-manifests/${id}`, { method: "DELETE", signal });
    return { err: raiseIfRefused(data).err };
  },
};

/** Mirrors entities.ConnectorManifest. */
export interface ApiConnectorManifest {
  id: string;
  key: string;
  name?: string;
  version?: number;
  enabled: boolean;
}
