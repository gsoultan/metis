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
    multi_instance_type: (d['multiInstanceType'] as string) || '',
    loop_cardinality: (d['loopCardinality'] as number) || 0,
    collection: (d['collection'] as string) || '',
    element_variable: (d['elementVariable'] as string) || '',
    completion_condition: (d['completionCondition'] as string) || '',
    is_event_sub_process: (d['isEventSubProcess'] as boolean) || false,
    condition: (d['condition'] as string) || (d['duration'] as string) || (d['script'] as string) || '',
    properties: {
      ...(d['properties'] as Record<string, unknown>),
      implementation: d['implementation'],
      connector_instance_id: d['connector_instance_id'],
      lock_duration: d['lockDuration'],
      http_url: d['httpUrl'],
      http_method: d['httpMethod'],
      headers: d['headers'],
      input_mapping: d['inputMapping'],
      output_mapping: d['outputMapping'],
      result_variable: d['resultVariable'],
      event_type: d['eventType'],
      timer_type: d['timerType'],
      timer_duration: d['duration'],
      signal_name: d['signalName'],
      message_name: d['messageName'],
      correlation_key: d['correlationKey'],
      form_definition: d['formDefinition'],
    },
  };
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

