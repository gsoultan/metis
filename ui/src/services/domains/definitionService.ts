import { definitionClient, statsClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type {
  CreateDefinitionPayload,
  ExportDefinitionResponse,
  ImportDefinitionResponse,
} from "../types";

export const definitionService = {
  async listDefinitions(projectId: string, signal?: AbortSignal) {
    const response = await definitionClient.listDefinitions({ projectId }, { signal });
    return { definitions: response.definitions ?? [], err: response.error };
  },

  async createDefinition(projectId: string, definition: CreateDefinitionPayload, signal?: AbortSignal) {
    const response = await definitionClient.createDefinition({
      projectId,
      key: definition?.key ?? "",
      name: definition?.name ?? "",
      // The generated request type models nodes and flows as messages with
      // nested relationships; the payload we build is structurally compatible
      // but not nominally, so it is asserted at this one boundary rather than
      // widening CreateDefinitionPayload to match generated code.
      nodes: (definition?.nodes ?? []) as never,
      flows: (definition?.flows ?? []) as never,
    }, { signal });

    return { id: response.id, err: response.error };
  },

  async getDefinition(_projectId: string, id: string, signal?: AbortSignal) {
    const response = await definitionClient.getDefinition({ id }, { signal });
    return { definition: response.definition, err: response.error };
  },

  async deleteDefinition(id: string, signal?: AbortSignal) {
    const response = await definitionClient.deleteDefinition({ id }, { signal });
    return { err: response.error };
  },

  async exportDefinition(id: string, signal?: AbortSignal) {
    return requestJSON<ExportDefinitionResponse>(`/definitions/${id}/export`, {
      method: "GET",
      signal,
    });
  },

  async importDefinition(xml: string, signal?: AbortSignal) {
    return requestJSON<ImportDefinitionResponse>("/definitions/import", {
      method: "POST",
      body: { xml: btoa(xml) },
      signal,
    });
  },

  async getProcessStatistics(projectId: string, signal?: AbortSignal) {
    const response = await statsClient.getProcessStatistics({ projectId }, { signal });
    return { stats: response, err: response.error };
  },
};
