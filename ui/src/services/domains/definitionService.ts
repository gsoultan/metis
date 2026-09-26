import type { JsonObject } from "@bufbuild/protobuf";
import type { ProcessDefinition } from "../../gen/entities/definition_pb";
import { definitionClient, statsClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type {
  ApiDefinition,
  ApiNodeAction,
  CreateDefinitionPayload,
  CreateFlowPayload,
  CreateNodePayload,
  ExportDefinitionResponse,
  ImportDefinitionResponse,
  CancelScheduledDefinitionResponse,
  ListDefinitionVersionsResponse,
  ListLiveVersionsResponse,
  MigrateInstancesResponse,
  PromoteDefinitionResponse,
  ScheduleDefinitionResponse,
} from "../types";
import { raiseIfRefused } from "../raise";
import { toBase64 } from "../shared/bytes";

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
  async listDefinitions(
    projectId: string,
    page?: { page: number; pageSize: number },
    signal?: AbortSignal,
  ) {
    const response = await definitionClient.listDefinitions(
      { projectId, page: page ? { page: page.page, pageSize: page.pageSize } : undefined },
      { signal },
    );
    return {
      definitions: response.definitions ?? [],
      err: response.error,
      // The window the server actually served, after clamping, so the controls
      // reflect what came back rather than what was asked for.
      pageInfo: response.page
        ? {
            total: Number(response.page.total),
            page: response.page.page,
            pageSize: response.page.pageSize,
            hasMore: response.page.hasMore,
          }
        : undefined,
    };
  },

  /**
   * Deploys a version.
   *
   * `stage` decides whether it goes live. Staged, the version is saved and
   * numbered but new instances keep starting on whichever version is live now —
   * so a model can be reviewed before it starts taking real work. Either way,
   * instances already running are untouched: they finish on their own version.
   */
  async createDefinition(
    projectId: string,
    definition: CreateDefinitionPayload,
    options?: { stage?: boolean },
    signal?: AbortSignal,
  ) {
    const response = await definitionClient.createDefinition({
      projectId,
      key: definition?.key ?? "",
      name: definition?.name ?? "",
      nodes: (definition?.nodes ?? []).map(toNodeMessage),
      flows: (definition?.flows ?? []).map(toFlowMessage),
      stage: options?.stage ?? false,
    }, { signal });

    const accepted = raiseIfRefused(response);
    return { id: accepted.id, version: accepted.version, live: accepted.live };
  },

  /**
   * Every version deployed under one process key, newest first, with which one
   * is live and how much work each still holds.
   */
  async listDefinitionVersions(projectId: string, key: string, signal?: AbortSignal) {
    const response = await requestJSON<ListDefinitionVersionsResponse>(
      `/definitions/versions?project_id=${encodeURIComponent(projectId)}&key=${encodeURIComponent(key)}`,
      { method: "GET", signal },
    );
    return { versions: response.versions ?? [], err: response.err };
  },

  /**
   * Arranges for a version to take over at a given time.
   *
   * `activateAt` must be in the future; the server refuses a time already gone
   * rather than treating it as "now", because those are different acts.
   */
  async scheduleDefinitionVersion(
    projectId: string,
    key: string,
    version: number,
    activateAt: Date,
    signal?: AbortSignal,
  ) {
    const response = await requestJSON<ScheduleDefinitionResponse>(
      `/definitions/versions/schedule`,
      {
        method: "POST",
        // RFC 3339 in UTC. Sending the browser's local time would make the
        // cutover depend on where the person arranging it happened to be.
        body: { project_id: projectId, key, version, activate_at: activateAt.toISOString() },
        signal,
      },
    );
    return { err: raiseIfRefused(response).err };
  },

  /** Drops a cutover that has not happened yet. */
  async cancelScheduledVersion(projectId: string, releaseId: string, signal?: AbortSignal) {
    const response = await requestJSON<CancelScheduledDefinitionResponse>(
      `/definitions/versions/schedule/cancel`,
      { method: "POST", body: { project_id: projectId, release_id: releaseId }, signal },
    );
    return { err: raiseIfRefused(response).err };
  },

  /**
   * Which version of each process key in a project is live.
   *
   * One call for a page that lists many processes; the per-key endpoint above
   * would be a request per row.
   */
  async listLiveVersions(projectId: string, signal?: AbortSignal) {
    const response = await requestJSON<ListLiveVersionsResponse>(
      `/definitions/live-versions?project_id=${encodeURIComponent(projectId)}`,
      { method: "GET", signal },
    );
    return { live: response.live ?? {}, err: response.err };
  },

  /**
   * Makes one deployed version the one new instances start on.
   *
   * Running instances are deliberately not moved. There is no safe general way
   * to relocate a token from one graph onto another, so a version change is a
   * cutover for new work only.
   */
  async promoteDefinitionVersion(projectId: string, key: string, version: number, signal?: AbortSignal) {
    const response = await requestJSON<PromoteDefinitionResponse>(
      `/definitions/versions/promote`,
      { method: "POST", body: { project_id: projectId, key, version }, signal },
    );
    return { err: raiseIfRefused(response).err };
  },

  /**
   * Moves running instances onto another version, or says what that would do.
   *
   * `dryRun` is the default at the call site for a reason: this rewrites
   * instances that have already been started — somebody's purchase order,
   * somebody's leave request — so the plan is what you get unless you ask to
   * commit. The reply carries the plan either way, so a refused apply explains
   * itself with the same numbers the preview showed.
   */
  async migrateInstances(
    sourceDefinitionId: string,
    targetDefinitionId: string,
    nodeMapping: Record<string, string>,
    dryRun = true,
    /**
     * Control-bearing steps whose loss the caller accepts, named one by one.
     *
     * Sent on the dry run too, so the preview shows the same refusals the apply
     * would make. A preview that is friendlier than the apply is how somebody
     * comes to press a button that then fails.
     */
    acknowledge: string[] = [],
    /** Nodes whose work is decided rather than moved, keyed by source node id. */
    nodeActions: Record<string, ApiNodeAction> = {},
    signal?: AbortSignal,
  ) {
    const response = await requestJSON<MigrateInstancesResponse>(`/definitions/versions/migrate`, {
      method: "POST",
      body: {
        source_definition_id: sourceDefinitionId,
        target_definition_id: targetDefinitionId,
        node_mapping: nodeMapping,
        node_actions: nodeActions,
        acknowledge: acknowledge,
        dry_run: dryRun,
      },
      signal,
    });
    // Not raiseIfRefused: a refusal here is the answer, not a failure. The plan
    // lists what would strand, and throwing it away would leave the person
    // fixing the mapping with a toast and no detail.
    return { plan: response.plan, applied: response.applied ?? false, err: response.err };
  },

  async getDefinition(_projectId: string, id: string, signal?: AbortSignal) {
    const response = await definitionClient.getDefinition({ id }, { signal });
    return { definition: fromDefinitionMessage(response.definition), err: response.error };
  },

  async deleteDefinition(id: string, signal?: AbortSignal) {
    const response = await definitionClient.deleteDefinition({ id }, { signal });
    return { err: raiseIfRefused(response).error };
  },

  async exportDefinition(id: string, signal?: AbortSignal) {
    return requestJSON<ExportDefinitionResponse>(`/definitions/${id}/export`, {
      method: "GET",
      signal,
    });
  },

  /**
   * Imports a BPMN model into a project.
   *
   * The project is required: the endpoint refuses a request without one, and
   * this used to send none, so every import failed with "project_id must be a
   * UUID".
   */
  async importDefinition(projectId: string, xml: string, signal?: AbortSignal) {
    return requestJSON<ImportDefinitionResponse>("/definitions/import", {
      method: "POST",
      body: { project_id: projectId, xml: toBase64(xml) },
      signal,
    });
  },

  async getProcessStatistics(projectId: string, signal?: AbortSignal) {
    const response = await statsClient.getProcessStatistics({ projectId }, { signal });
    return { stats: response, err: response.error };
  },
};
