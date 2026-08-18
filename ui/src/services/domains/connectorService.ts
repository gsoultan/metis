import { requestJSON } from "../shared/rest";
import type {
  ApiConnector,
  ApiConnectorInstance,
  CreateConnectorInstancePayload,
  CreateConnectorPayload,
} from "../types";

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
    return { connector: data.connector, err: data.err };
  },

  async updateConnector(connector: ApiConnector, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/${connector.id}`, {
      method: "PUT",
      body: { connector },
      signal,
    });
    return { err: data.err };
  },

  async deleteConnector(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/${id}`, {
      method: "DELETE",
      signal,
    });
    return { err: data.err };
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
    return { instance: data.instance, err: data.err };
  },

  async updateConnectorInstance(instance: ApiConnectorInstance, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/instances/${instance.id}`, {
      method: "PUT",
      body: { instance },
      signal,
    });
    return { err: data.err };
  },

  async deleteConnectorInstance(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/connectors/instances/${id}`, {
      method: "DELETE",
      signal,
    });
    return { err: data.err };
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

    if (data.err) {
      throw new Error(data.err);
    }

    return data.result;
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

    if (data.err) {
      throw new Error(data.err);
    }

    return data.variables;
  },

  /** The connectors installed as documents rather than compiled in. */
  async listConnectorManifests(signal?: AbortSignal) {
    const data = await requestJSON<{ manifests?: ApiConnectorManifest[]; err?: string }>(
      "/connector-manifests",
      { signal },
    );
    return data.manifests ?? [];
  },

  /**
   * Installs one.
   *
   * `format` is "manifest" or "openapi" — one endpoint for both, because what a
   * person has in front of them is "a file the vendor published" and being asked
   * which upload button it belongs to is a question about our implementation.
   */
  async installConnectorManifest(document: string, format: "manifest" | "openapi", signal?: AbortSignal) {
    const data = await requestJSON<{ manifests?: ApiConnectorManifest[]; err?: string }>(
      "/connector-manifests",
      { method: "POST", body: JSON.stringify({ document, format }), signal },
    );
    return data.manifests ?? [];
  },

  async setConnectorManifestEnabled(id: string, enabled: boolean, signal?: AbortSignal) {
    return requestJSON<{ err?: string }>(`/connector-manifests/${id}/enabled`, {
      method: "POST",
      body: JSON.stringify({ enabled }),
      signal,
    });
  },

  async deleteConnectorManifest(id: string, signal?: AbortSignal) {
    return requestJSON<{ err?: string }>(`/connector-manifests/${id}`, { method: "DELETE", signal });
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
