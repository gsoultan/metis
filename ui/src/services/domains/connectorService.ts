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
};
