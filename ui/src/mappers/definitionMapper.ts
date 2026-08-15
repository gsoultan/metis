/**
 * Typed bi-directional mapper for BPMN process definitions.
 *
 * FE-ARCH-4: mapLoadedNodes / mapLoadedEdges now accept the typed ApiNode /
 * ApiFlow contracts from src/services/types instead of any[].
 *
 * FE-ARCH-6: buildDefinitionPayload is extracted here and has fully typed
 * input (Node<BPMNNodeData>[]) and output (CreateDefinitionPayload).
 */
import type { Edge, Node } from '@xyflow/react';
import type { BPMNEdgeData, BPMNNodeData } from '../types/bpmn';
import type {
  ApiFlow,
  ApiNode,
  CreateDefinitionPayload,
  CreateFlowPayload,
  CreateNodePayload,
} from '../services/types';

// ─── Server → React Flow ─────────────────────────────────────────────────────

/** Map server-side ApiNode objects to React Flow nodes with typed BPMNNodeData. */
export function mapLoadedNodes(rawNodes: ApiNode[] = []): Node<BPMNNodeData>[] {
  return rawNodes.map((node) => ({
    id: node.id,
    type: node.type,
    position: { x: node.x, y: node.y },
    // The data object intentionally spreads node.properties last so unknown
    // server-side keys are preserved in the properties bag without polluting
    // the typed surface.
    data: {
      // Every stored setting, under the name the server uses. The property
      // editors read several of them directly — decision_key, input_mapping,
      // connector_id — and without this they opened blank for a node that was
      // configured, which reads as "not set" and invites setting it again.
      ...(node.properties ?? {}),
      // Needed by the discriminated union but we use a cast because the server
      // sends a plain string and the union covers all known values.
      nodeType: node.type as BPMNNodeData['nodeType'],
      label: node.name,
      assignee: node.assignee,
      candidateUsers: (node.candidate_users ?? []).map((u) => (u as { username?: string }).username ?? ''),
      candidateGroups: (node.candidate_groups ?? []).map((g) => (g as { name?: string }).name ?? ''),
      priority: node.priority,
      dueDate: node.due_date,
      formKey: node.form_key,
      defaultFlow: node.default_flow,
      script: node.script,
      scriptFormat: node.script_format,
      externalTopic: node.external_topic,
      documentation: node.documentation,
      // The API has carried both spellings; accept either without widening the node type.
      attachedToRef: (node as { attached_to_ref?: string; attachedToRef?: string }).attached_to_ref
        ?? (node as { attachedToRef?: string }).attachedToRef,
      parentId: node.parent_id,
      cancelActivity: node.cancel_activity,
      errorCode: node.error_code,
      multiInstanceType: node.multi_instance_type,
      loopCardinality: node.loop_cardinality,
      collection: node.collection,
      elementVariable: node.element_variable,
      completionCondition: node.completion_condition,
      isEventSubProcess: node.is_event_sub_process,
      // Properties extracted from the server property bag
      implementation: node.properties?.implementation as string | undefined,
      connector_instance_id: node.properties?.connector_instance_id as string | undefined,
      lockDuration: node.properties?.lock_duration as string | undefined,
      httpUrl: node.properties?.http_url as string | undefined,
      httpMethod: node.properties?.http_method as string | undefined,
      headers: node.properties?.headers as string | undefined,
      inputMapping: node.properties?.input_mapping as string | undefined,
      outputMapping: node.properties?.output_mapping as string | undefined,
      resultVariable: node.properties?.result_variable as string | undefined,
      eventType: node.properties?.event_type as string | undefined,
      timerType: node.properties?.timer_type as 'duration' | 'date' | 'cycle' | undefined,
      duration: (node.properties?.timer_duration ?? node.condition) as string | undefined,
      signalName: node.properties?.signal_name as string | undefined,
      messageName: node.properties?.message_name as string | undefined,
      correlationKey: node.properties?.correlation_key as string | undefined,
      escalationCode: node.properties?.escalation_code as string | undefined,
      activityRef: node.properties?.activity_ref as string | undefined,
      nonInterrupting: node.properties?.non_interrupting as boolean | undefined,
      formDefinition: node.properties?.form_definition,
      // Raw property bag for round-trip preservation
      properties: node.properties,
    } as BPMNNodeData,
  }));
}

/** Map server-side ApiFlow objects to React Flow edges with typed BPMNEdgeData. */
export function mapLoadedEdges(rawFlows: ApiFlow[] = []): Edge<BPMNEdgeData>[] {
  return rawFlows.map((flow) => ({
    id: flow.id,
    source: flow.source_ref,
    target: flow.target_ref,
    label: flow.condition,
    animated: true,
    style: { strokeWidth: 2 },
    data: {
      documentation: flow.documentation,
      condition: flow.condition,
    },
  }));
}

// ─── React Flow → Server ─────────────────────────────────────────────────────

/** Map a single React Flow node to the server-side CreateNodePayload. */
function mapNodeToPayload(node: Node<BPMNNodeData>): CreateNodePayload {
  const d = node.data;
  return {
    id: node.id,
    name: (d['label'] as string) || '',
    type: node.type || 'userTask',
    x: Math.round(node.position.x),
    y: Math.round(node.position.y),
    assignee: (d['assignee'] as string) || '',
    candidate_users: (d['candidateUsers'] as string[]) || [],
    candidate_groups: (d['candidateGroups'] as string[]) || [],
    priority: (d['priority'] as number) || 0,
    due_date: (d['dueDate'] as string) || '',
    form_key: (d['formKey'] as string) || '',
    default_flow: (d['defaultFlow'] as string) || '',
    script: (d['script'] as string) || '',
    script_format: (d['scriptFormat'] as string) || '',
    external_topic: (d['externalTopic'] as string) || '',
    documentation: (d['documentation'] as string) || '',
    attached_to_ref: (d['attachedToRef'] as string) || '',
    parent_id: (d['parentId'] as string) || '',
    cancel_activity: (d['cancelActivity'] as boolean) || false,
    error_code: (d['errorCode'] as string) || '',
    multi_instance_type: (d['multiInstanceType'] as string) || '',
    loop_cardinality: (d['loopCardinality'] as number) || 0,
    collection: (d['collection'] as string) || '',
    element_variable: (d['elementVariable'] as string) || '',
    completion_condition: (d['completionCondition'] as string) || '',
    is_event_sub_process: (d['isEventSubProcess'] as boolean) || false,
    condition: (d['condition'] as string) || (d['duration'] as string) || (d['script'] as string) || '',
    properties: nodeProperties(d),
  };
}

/**
 * Everything on a node that is configuration rather than canvas state.
 *
 * This used to be a hand-written list of the settings the mapper knew about,
 * and the property editors had drifted away from it: they wrote decision_key,
 * decision_version, input_mapping, output_mapping, connector_id,
 * called_process_key, topic, url and auth_token, none of which the list
 * mentioned. Those settings were dropped on save, silently — the panel showed
 * them, the process ran without them. Choosing the decision for a business rule
 * task did nothing at all, which is most of the point of having one.
 *
 * So the rule is inverted. Anything the editors put on a node is configuration
 * unless it is either the designer's own business or has a column of its own on
 * the payload, and the aliases below only exist to give a camelCase editor field
 * the name the server stores it under.
 */
const CANVAS_ONLY_KEYS = new Set([
  'label', 'nodeType', 'documentation', 'status', 'heatmapValue', 'properties',
  // These have their own field on CreateNodePayload, set above.
  'assignee', 'candidateUsers', 'candidateGroups', 'priority', 'dueDate',
  'formKey', 'defaultFlow', 'script', 'scriptFormat', 'externalTopic',
  'attachedToRef', 'parentId', 'cancelActivity', 'errorCode', 'multiInstanceType',
  'loopCardinality', 'collection', 'elementVariable', 'completionCondition',
  'isEventSubProcess', 'condition',
]);

/** Editor field name → the name the server stores the setting under. */
const PROPERTY_ALIASES: Record<string, string> = {
  connectorInstanceId: 'connector_instance_id',
  lockDuration: 'lock_duration',
  httpUrl: 'http_url',
  httpMethod: 'http_method',
  inputMapping: 'input_mapping',
  outputMapping: 'output_mapping',
  resultVariable: 'result_variable',
  eventType: 'event_type',
  timerType: 'timer_type',
  duration: 'timer_duration',
  signalName: 'signal_name',
  messageName: 'message_name',
  correlationKey: 'correlation_key',
  escalationCode: 'escalation_code',
  activityRef: 'activity_ref',
  nonInterrupting: 'non_interrupting',
  formDefinition: 'form_definition',
  decisionKey: 'decision_key',
  decisionVersion: 'decision_version',
  calledProcessKey: 'called_process_key',
  calledProcessVersion: 'called_process_version',
};

function nodeProperties(d: BPMNNodeData): Record<string, unknown> {
  const out: Record<string, unknown> = { ...(d['properties'] as Record<string, unknown> ?? {}) };
  for (const [key, value] of Object.entries(d)) {
    if (value === undefined || CANVAS_ONLY_KEYS.has(key)) continue;
    out[PROPERTY_ALIASES[key] ?? key] = value;
  }
  return out;
}

/** Map a single React Flow edge to the server-side CreateFlowPayload. */
function mapEdgeToPayload(edge: Edge<BPMNEdgeData>): CreateFlowPayload {
  return {
    id: edge.id,
    source_ref: edge.source,
    target_ref: edge.target,
    condition: (edge.data?.condition as string) ?? (edge.label as string) ?? '',
    documentation: (edge.data?.documentation as string) ?? '',
  };
}

/**
 * Build the full CreateDefinitionPayload from the React Flow canvas state.
 *
 * FE-ARCH-6: This replaces the inline function that was embedded in
 * useProcessDesigner.ts.  The typed input prevents missing fields silently.
 */
export function buildDefinitionPayload(
  processName: string,
  processKey: string,
  nodes: Node<BPMNNodeData>[],
  edges: Edge<BPMNEdgeData>[],
): CreateDefinitionPayload {
  return {
    key: processKey,
    name: processName,
    nodes: nodes.map(mapNodeToPayload),
    flows: edges.map(mapEdgeToPayload),
  };
}

