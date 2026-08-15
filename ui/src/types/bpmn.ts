/**
 * Strict TypeScript types for BPMN node and edge data stored in React Flow.
 *
 * FE-ARCH-3: replaces all `any` casts for node.data and edge.data.
 *
 * Usage:
 *   import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';
 *   const nodes: Node<BPMNNodeData>[] = [...];
 */

// ─── Shared base ─────────────────────────────────────────────────────────────

/** Properties every node type exposes regardless of specialisation. */
export interface BaseBPMNNodeData {
  label: string;
  documentation?: string;
  /** Raw property bag preserved from the server round-trip. */
  properties?: Record<string, unknown>;
  /** Set by the instance viewer to show execution state. */
  status?: 'active' | 'completed' | 'pending';
  /** Heatmap overlay value (execution frequency). */
  heatmapValue?: number;
  [key: string]: unknown;
}

/** Multi-instance configuration shared by several task types. */
export interface MultiInstanceConfig {
  multiInstanceType?: 'parallel' | 'sequential' | 'none';
  loopCardinality?: number;
  collection?: string;
  elementVariable?: string;
  completionCondition?: string;
}

// ─── Task variants ────────────────────────────────────────────────────────────

export interface UserTaskData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'userTask';
  assignee?: string;
  candidateUsers?: string[];
  candidateGroups?: string[];
  priority?: number;
  dueDate?: string;
  formKey?: string;
  formDefinition?: unknown;
}

export interface ManualTaskData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'manualTask';
  assignee?: string;
}

export interface ServiceTaskData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'serviceTask';
  /** External worker topic (pull model). */
  externalTopic?: string;
  implementation?: string;
  connector_instance_id?: string;
  lockDuration?: string;
  httpUrl?: string;
  httpMethod?: string;
  headers?: string;
  inputMapping?: string;
  outputMapping?: string;
  resultVariable?: string;
}

export interface ScriptTaskData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'scriptTask';
  script?: string;
  scriptFormat?: string;
}

export interface BusinessRuleTaskData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'businessRuleTask';
  decisionKey?: string;
  decisionVersion?: number;
  inputMapping?: string;
  outputMapping?: string;
}

export interface CallActivityData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'callActivity';
  calledElement?: string;
  calledElementVersion?: number;
}

// ─── Gateway variants ─────────────────────────────────────────────────────────

export interface GatewayData extends BaseBPMNNodeData {
  nodeType: 'exclusiveGateway' | 'inclusiveGateway' | 'parallelGateway' | 'eventBasedGateway';
  defaultFlow?: string;
}

// ─── Event variants ───────────────────────────────────────────────────────────

export interface EventData extends BaseBPMNNodeData {
  nodeType:
    | 'startEvent'
    | 'endEvent'
    | 'intermediateCatchEvent'
    | 'intermediateThrowEvent'
    | 'boundaryEvent'
    | 'signalEvent'
    | 'messageEvent'
    | 'timerEvent';
  eventType?: string;
  timerType?: 'duration' | 'date' | 'cycle';
  /** Timer duration or ISO date expression. Also used as the condition field. */
  duration?: string;
  signalName?: string;
  messageName?: string;
  correlationKey?: string;
  /** Boundary event: reference to the host activity. */
  attachedToRef?: string;
  /** Boundary event: whether the attached activity is cancelled. */
  cancelActivity?: boolean;
  /**
   * Boundary event: notify without stopping the work it is attached to.
   *
   * Interrupting is the default, both in BPMN and in every definition already
   * stored, so this is an explicit opt-in rather than a reading of
   * cancelActivity.
   */
  nonInterrupting?: boolean;
  /** Error boundary event: which failure it catches. Empty catches any. */
  errorCode?: string;
  /** Escalation event: which situation is being raised or caught. */
  escalationCode?: string;
  /** Compensation throw: the one activity to undo. Empty undoes them all. */
  activityRef?: string;
}

// ─── Sub-process ──────────────────────────────────────────────────────────────

export interface SubProcessData extends BaseBPMNNodeData, MultiInstanceConfig {
  nodeType: 'subProcess';
  isEventSubProcess?: boolean;
  parentId?: string;
}

// ─── Discriminated union ──────────────────────────────────────────────────────

export type BPMNNodeData =
  | UserTaskData
  | ManualTaskData
  | ServiceTaskData
  | ScriptTaskData
  | BusinessRuleTaskData
  | CallActivityData
  | GatewayData
  | EventData
  | SubProcessData;

// ─── Edge data ────────────────────────────────────────────────────────────────

export interface BPMNEdgeData {
  /** Condition expression displayed on the edge (used by gateways). */
  condition?: string;
  documentation?: string;
  [key: string]: unknown;
}


// ─── Reading settings off node data ──────────────────────────────────────────

/**
 * BaseBPMNNodeData carries an index signature so the property editors can hold
 * settings the union does not name — and every read through it is `unknown`.
 * These narrow at the point of use, so an editor binds a value without each one
 * inventing its own cast, and a setting stored as the wrong type shows as empty
 * rather than reaching a control that cannot render it.
 */
export function asText(value: unknown, fallback = ''): string {
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return fallback;
}

export function asNumber(value: unknown, fallback = 0): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return fallback;
}

export function asBoolean(value: unknown): boolean {
  return value === true || value === 'true';
}

export function asTextList(value: unknown): string[] {
  return Array.isArray(value) ? value.map((v) => asText(v)) : [];
}

/** A mapping editor holds name → name pairs. */
export function asTextMap(value: unknown): Record<string, string> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>).map(([k, v]) => [k, asText(v)]),
  );
}
