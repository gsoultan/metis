import type { JsonObject } from "@bufbuild/protobuf";
import type { ProcessDefinition } from "../../gen/entities/definition_pb";
import { definitionClient, statsClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type {
  ApiDefinition,
  CreateDefinitionPayload,
  CreateFlowPayload,
  CreateNodePayload,
  ExportDefinitionResponse,
  ImportDefinitionResponse,
} from "../types";

/**
 * The designer works in the shapes a person edits — an assignee is a username,
 * a candidate group is a group name — while the wire format uses the same
 * messages the rest of the API does. These two functions are where that is
 * translated.
 *
 * This was previously two `as never` casts, on the reasoning that the shapes
 * were structurally compatible. They were not: the wire format names a flow's
 * ends `sourceRef`/`targetRef`, the payload names them `source_ref`/
 * `target_ref`, and unknown fields are dropped rather than rejected. Every flow
 * arrived with both ends empty and the server refused the definition with
 * "Sequence flow f1 has no source reference" — the cast silenced the one check
 * that would have caught it.
 */
function toNodeMessage(n: CreateNodePayload) {
  return {
    id: n.id,
    name: n.name,
    type: n.type ?? "",
    assignee: n.assignee ? { username: n.assignee } : undefined,
    candidateUsers: (n.candidate_users ?? []).map((username) => ({ username })),
    candidateGroups: (n.candidate_groups ?? []).map((name) => ({ name })),

    // The settings that make a node do anything: decision_key, http_url, the
    // input_/output_ mappings, form_definition.
    properties: (n.properties ?? {}) as JsonObject,

    documentation: n.documentation,
    formKey: n.form_key,
    defaultFlow: n.default_flow,
    script: n.script,
    scriptFormat: n.script_format,
    externalTopic: n.external_topic,
    priority: n.priority,
    dueDate: n.due_date,
    condition: n.condition,

    attachedToRef: n.attached_to_ref,
    parentId: n.parent_id,
    cancelActivity: n.cancel_activity,
    isEventSubProcess: n.is_event_sub_process,

    multiInstanceType: n.multi_instance_type,
    loopCardinality: n.loop_cardinality,
    collection: n.collection,
    elementVariable: n.element_variable,
    completionCondition: n.completion_condition,

    x: n.x,
    y: n.y,
  };
}

function toFlowMessage(f: CreateFlowPayload) {
  return {
    id: f.id,
    sourceRef: f.source_ref,
    targetRef: f.target_ref,
    condition: f.condition,
    documentation: f.documentation,
  };
}

/**
 * The inbound half of the same translation.
 *
 * Protobuf field names arrive camelCased, and the designer's mapper reads the
 * snake_case names the REST API uses. Reading a definition back returned no
 * nodes at all until now, so the two never had to agree; they do from here, and
 * the disagreement would show up as a process that reopens with its arrows
 * disconnected rather than as an error.
 */
function fromDefinitionMessage(d: ProcessDefinition | undefined): ApiDefinition | undefined {
  if (!d) return undefined;
  return {
    id: d.id,
    project_id: d.project?.id ?? "",
    key: d.key,
    name: d.name,
    version: d.version,
    nodes: (d.nodes ?? []).map((n) => ({
      id: n.id,
      name: n.name,
      type: n.type,
      x: n.x,
      y: n.y,
      assignee: n.assignee?.username,
      candidate_users: (n.candidateUsers ?? []).map((u) => ({ username: u.username })),
      candidate_groups: (n.candidateGroups ?? []).map((g) => ({ name: g.name })),
      priority: n.priority,
      due_date: n.dueDate,
      form_key: n.formKey,
      default_flow: n.defaultFlow,
      script: n.script,
      script_format: n.scriptFormat,
      external_topic: n.externalTopic,
      documentation: n.documentation,
      attached_to_ref: n.attachedToRef,
      parent_id: n.parentId,
      cancel_activity: n.cancelActivity,
      multi_instance_type: n.multiInstanceType,
      loop_cardinality: n.loopCardinality,
      collection: n.collection,
      element_variable: n.elementVariable,
      completion_condition: n.completionCondition,
      is_event_sub_process: n.isEventSubProcess,
      condition: n.condition,
      properties: n.properties as Record<string, unknown> | undefined,
    })),
    flows: (d.flows ?? []).map((f) => ({
      id: f.id,
      source_ref: f.sourceRef,
      target_ref: f.targetRef,
      condition: f.condition,
      documentation: f.documentation,
    })),
  };
}

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
      nodes: (definition?.nodes ?? []).map(toNodeMessage),
      flows: (definition?.flows ?? []).map(toFlowMessage),
    }, { signal });

    return { id: response.id, err: response.error };
  },

  async getDefinition(_projectId: string, id: string, signal?: AbortSignal) {
    const response = await definitionClient.getDefinition({ id }, { signal });
    return { definition: fromDefinitionMessage(response.definition), err: response.error };
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
