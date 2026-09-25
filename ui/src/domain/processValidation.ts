/**
 * What is wrong with a process, before it is deployed.
 *
 * There used to be two of these. The one the Deploy button consulted checked
 * only that a start and an end existed and that nodes had arrows; a stricter
 * one — reachability, dead ends, gateways with unconditioned paths — sat in
 * `components/processDiagnostics.ts` and **was never called by anything**, and
 * would have named steps by an id anyway because it read a `name` field the
 * canvas does not set.
 *
 * So a process could be deployed with a gateway whose paths carried no
 * conditions at all. The server refuses to guess a branch (the implicit default
 * flow ships off), so the first instance to reach that gateway raises an
 * incident and stops. The designer said nothing.
 *
 * These checks are pure functions over the canvas so they can be tested without
 * a browser, and every message names the step the way the canvas does and says
 * what to do about it.
 */

import { NODE_VOCABULARY, isEndingNode, vocabularyFor } from './bpmnVocabulary';
import { missingStepFields, type StepSchemas, unmappedParameters } from './connectorStep';

export type IssueSeverity = 'error' | 'warning';

export interface ValidationIssue {
  message: string;
  severity: IssueSeverity;
  /** The node or edge the issue is about, so the panel can select it. */
  id?: string;
  /** What to do about it. */
  suggestion?: string;
}

/** The parts of a canvas node these checks read. */
export interface CheckableNode {
  id: string;
  type?: string;
  data?: { label?: unknown; defaultFlow?: unknown; connector_id?: unknown };
}

/** The parts of a canvas edge these checks read. */
export interface CheckableEdge {
  id: string;
  source: string;
  target: string;
  data?: { condition?: unknown };
}

/**
 * What to call a step in a message.
 *
 * The name the author gave it, falling back to what that kind of step is called
 * in plain language, and only then to the id. An id in an error message is a
 * dead end for the person reading it.
 */
export function stepName(node: CheckableNode): string {
  const label = typeof node.data?.label === 'string' ? node.data.label.trim() : '';
  if (label) return label;
  const plain = vocabularyFor(node.type ?? '')?.plainName;
  return plain ?? node.id;
}

function isGateway(type: string | undefined): boolean {
  return typeof type === 'string' && type.toLowerCase().includes('gateway');
}

/** Gateways that pick one path by testing conditions. */
function choosesByCondition(type: string | undefined): boolean {
  return type === 'exclusiveGateway' || type === 'inclusiveGateway';
}

/**
 * What a step that fills in fields for its connector has left out.
 *
 * The server refuses to deploy a lookup with no query or nowhere to put its
 * answer, and fails one whose query names a value the step never gives. Saying
 * so here, on the step, is the difference between a red dot and a stack trace.
 */
function connectorStepIssues(nodes: CheckableNode[], stepSchemas: StepSchemas): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  for (const node of nodes) {
    const connectorId = typeof node.data?.connector_id === 'string' ? node.data.connector_id : '';
    const schema = stepSchemas.get(connectorId);
    if (!schema || !node.data) continue;
    const data = node.data as Record<string, unknown>;

    const missing = missingStepFields(data, schema);
    if (missing.length > 0) {
      issues.push({
        message: `"${stepName(node)}" is missing ${missing.map((field) => `“${field.label}”`).join(' and ')}.`,
        severity: 'error',
        id: node.id,
        suggestion: 'Fill it in on the step.',
      });
    }
    const unmapped = unmappedParameters(data);
    if (unmapped.length > 0) {
      issues.push({
        message: `The query in "${stepName(node)}" uses ${unmapped.map((name) => `:${name}`).join(', ')} without saying where the value comes from.`,
        severity: 'error',
        id: node.id,
        suggestion: 'Give each one a value under Values on the step.',
      });
    }
  }
  return issues;
}

/**
 * Every name a step that calls another system can be pointed at something by.
 *
 * The property panel shows a web address from httpUrl, http_url or url, and a
 * topic from externalTopic, external_topic or topic (ServiceTaskConfig). Read
 * fewer here and the warning below would fire on a step the panel shows as set
 * up.
 */
const CALL_TARGET_KEYS = [
  'httpUrl', 'http_url', 'url',
  'connector_id', 'connector_instance_id', 'connectorInstanceId',
  'externalTopic', 'external_topic', 'topic',
];

function isFilledIn(value: unknown): boolean {
  return typeof value === 'string' && value.trim() !== '';
}

/**
 * Steps that call another system and are pointed at nothing.
 *
 * The engine does not treat that as a mistake. With no topic the step becomes
 * a job; with no connector the job falls through to the web call; and with no
 * web address the web call returns nothing, successfully. The instance carries
 * on as though the work were done, and nothing anywhere says it was not.
 *
 * A script does not count as something to call. The engine runs scripts only
 * on a "Work something out" step, and on this kind of step stores one and
 * ignores it. Telling the person who wrote it that the step "does not call
 * anything" would read as wrong, so they are told why instead.
 *
 * A warning, not an error: a placeholder is a legitimate way to sketch a
 * process before the system it calls exists.
 */
function unpointedServiceTaskIssues(nodes: CheckableNode[]): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  for (const node of nodes) {
    if (node.type !== 'serviceTask') continue;
    const data = (node.data ?? {}) as Record<string, unknown>;
    if (CALL_TARGET_KEYS.some((key) => isFilledIn(data[key]))) continue;
    issues.push(isFilledIn(data.script) ? ignoredScriptIssue(node) : callsNothingIssue(node));
  }
  return issues;
}

function callsNothingIssue(node: CheckableNode): ValidationIssue {
  return {
    message: `"${stepName(node)}" does not call anything, so the process would pass through it without doing the work.`,
    severity: 'warning',
    id: node.id,
    suggestion: 'Under “What it calls”, give it a web address, choose a connector, or name a topic for a worker to pick up.',
  };
}

function ignoredScriptIssue(node: CheckableNode): ValidationIssue {
  return {
    message: `The script in "${stepName(node)}" will never run: this kind of step only calls other systems, and it is not pointed at one.`,
    severity: 'warning',
    id: node.id,
    suggestion: `Move the script to a “${NODE_VOCABULARY.scriptTask.plainName}” step, which does run it.`,
  };
}

export function validateProcess(
  nodes: CheckableNode[],
  edges: CheckableEdge[],
  stepSchemas: StepSchemas = new Map(),
): ValidationIssue[] {
  if (nodes.length === 0) {
    return [{
      message: 'This process is empty.',
      severity: 'warning',
      suggestion: 'Add a Start, at least one step, and a Finish.',
    }];
  }

  const issues: ValidationIssue[] = [
    ...connectorStepIssues(nodes, stepSchemas),
    ...unpointedServiceTaskIssues(nodes),
  ];
  const starts = nodes.filter((n) => n.type === 'startEvent');

  if (starts.length === 0) {
    issues.push({
      message: 'This process has no starting point.',
      severity: 'error',
      suggestion: 'Add a Start step so the process knows where to begin.',
    });
  }

  if (!nodes.some((n) => isEndingNode(n.type))) {
    issues.push({
      message: 'This process never finishes.',
      severity: 'error',
      suggestion: 'Add a Finish step so an instance can complete instead of hanging.',
    });
  }

  // Which steps can actually be reached from a start.
  const outgoingBySource = new Map<string, CheckableEdge[]>();
  const incomingCount = new Map<string, number>();
  for (const edge of edges) {
    const list = outgoingBySource.get(edge.source);
    if (list) list.push(edge);
    else outgoingBySource.set(edge.source, [edge]);
    incomingCount.set(edge.target, (incomingCount.get(edge.target) ?? 0) + 1);
  }

  if (starts.length > 0) {
    const reached = new Set<string>();
    const stack = starts.map((s) => s.id);
    while (stack.length > 0) {
      const current = stack.pop() as string;
      if (reached.has(current)) continue;
      reached.add(current);
      for (const edge of outgoingBySource.get(current) ?? []) stack.push(edge.target);
    }
    for (const node of nodes) {
      if (reached.has(node.id)) continue;
      // A boundary event hangs off its activity rather than being flowed into,
      // so it is legitimately unreachable by arrows.
      if (typeof node.type === 'string' && node.type.toLowerCase().includes('boundary')) continue;
      issues.push({
        message: `"${stepName(node)}" can never be reached.`,
        severity: 'error',
        id: node.id,
        suggestion: 'Connect an arrow to it from a step that runs before it, or delete it.',
      });
    }
  }

  for (const node of nodes) {
    const outgoing = outgoingBySource.get(node.id) ?? [];
    const isBoundary = typeof node.type === 'string' && node.type.toLowerCase().includes('boundary');

    if (node.type !== 'startEvent' && !isBoundary && (incomingCount.get(node.id) ?? 0) === 0) {
      issues.push({
        message: `Nothing leads to "${stepName(node)}".`,
        severity: 'warning',
        id: node.id,
        suggestion: 'Draw an arrow into it from the step that comes before.',
      });
    }

    if (!isEndingNode(node.type) && outgoing.length === 0) {
      issues.push({
        message: `"${stepName(node)}" is a dead end.`,
        severity: 'warning',
        id: node.id,
        suggestion: 'Draw an arrow to what happens next, or use a Finish step to end here.',
      });
    }

    if (!isGateway(node.type)) continue;

    if (outgoing.length === 1 && choosesByCondition(node.type)) {
      issues.push({
        message: `"${stepName(node)}" only has one way out, so it decides nothing.`,
        severity: 'warning',
        id: node.id,
        suggestion: 'Give it a second path, or remove it and connect the steps directly.',
      });
    }

    if (!choosesByCondition(node.type) || outgoing.length < 2) continue;

    /*
     * The check that matters most.
     *
     * A path with no condition is only safe if it is the gateway's default. The
     * server does not guess: with no matching condition and no default it
     * raises an incident and the instance stops, by design. Deploying that is
     * shipping a process that fails the first time somebody uses it.
     */
    const defaultFlow = typeof node.data?.defaultFlow === 'string' ? node.data.defaultFlow : '';
    const unconditioned = outgoing.filter((edge) => {
      const condition = typeof edge.data?.condition === 'string' ? edge.data.condition.trim() : '';
      return condition === '' && edge.id !== defaultFlow;
    });

    if (unconditioned.length === 0) continue;

    const targets = unconditioned
      .map((edge) => nodes.find((n) => n.id === edge.target))
      .map((target) => (target ? `"${stepName(target)}"` : 'a step'))
      .join(', ');

    if (defaultFlow) {
      issues.push({
        message: `"${stepName(node)}" has paths with no condition: ${targets}.`,
        severity: 'error',
        id: node.id,
        suggestion: 'Say when each path is taken. Only the fallback path may be left blank.',
      });
    } else {
      issues.push({
        message: `"${stepName(node)}" does not say when to take ${targets}.`,
        severity: 'error',
        id: node.id,
        suggestion:
          'Give each path a condition, and pick one as the fallback for when none of them match. ' +
          'Without a fallback the process stops here with an incident.',
      });
    }
  }

  return issues;
}

/** Whether anything found would stop a deploy. */
export function hasBlockingIssues(issues: ValidationIssue[]): boolean {
  return issues.some((issue) => issue.severity === 'error');
}
