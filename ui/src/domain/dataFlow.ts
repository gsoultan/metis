/**
 * What data a process carries, at each step.
 *
 * A running process carries a single bag of variables: every step reads from it
 * and writes back to it. That model is written down in docs/data-flow.md, and
 * the commonest question about a process — "where does this value come from?" —
 * could only be answered by reading that page and tracing by hand.
 *
 * This computes the same thing from the diagram: for any step, what is in the
 * bag when it starts, what it puts in, and what is in the bag when it is done.
 * Nothing is executed; it reads the configuration each node already carries.
 */
import type { Edge, Node } from '@xyflow/react';

import type { BPMNEdgeData, BPMNNodeData } from '../types/bpmn';
import { asText } from '../types/bpmn';

/** One value in the bag, and where it came from. */
export interface Variable {
  name: string;
  /** The step that put it there, by name. */
  producedBy: string;
  /** The value the sample data gives it, when there is one. */
  sample?: unknown;
  /**
   * False when some path to this step does not produce it — a value set on one
   * branch of a gateway is not there if the other branch was taken, and reading
   * it is the commonest cause of "why is my variable empty".
   */
  always: boolean;
}

export interface NodeDataFlow {
  /** In the bag when this step starts. */
  before: Variable[];
  /** What this step puts in. */
  produces: Variable[];
  /** In the bag when this step is done. */
  after: Variable[];
}

/** The sample data an author put on the start event, if it parses. */
export function sampleDataOf(nodes: Node<BPMNNodeData>[]): Record<string, unknown> {
  for (const node of nodes) {
    if (node.type !== 'startEvent') continue;
    const raw = asText(node.data.sampleData);
    if (!raw.trim()) continue;
    try {
      const parsed: unknown = JSON.parse(raw);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      // An author half-way through typing is not an error worth reporting here;
      // the editor itself says whether the JSON is valid.
    }
  }
  return {};
}

/**
 * The names a step adds to the bag, read from how it is configured.
 *
 * Where a step's outputs cannot be known from the diagram — a decision table
 * lives elsewhere, an endpoint returns what it returns — the name is what the
 * author mapped it to, because that mapping is the part they control and the
 * part they will read later.
 */
function producedNames(node: Node<BPMNNodeData>, sample: Record<string, unknown>): string[] {
  const data = node.data;
  const properties = (data.properties ?? {}) as Record<string, unknown>;

  switch (node.type) {
    case 'startEvent':
      return Object.keys(sample);

    case 'serviceTask': {
      // output_<theirs> = <ours>: the names the response is stored under.
      const mapped = Object.entries(properties)
        .filter(([key]) => key.startsWith('output_'))
        .map(([, value]) => asText(value))
        .filter(Boolean);
      if (mapped.length > 0) return mapped;
      const result = asText(data.resultVariable);
      return result ? [result] : [];
    }

    case 'businessRuleTask': {
      const mapping = data.output_mapping ?? data.outputMapping;
      if (mapping && typeof mapping === 'object') {
        return Object.keys(mapping as Record<string, unknown>);
      }
      return [];
    }

    case 'scriptTask': {
      const result = asText(data.resultVariable);
      return result ? [result] : [];
    }

    case 'userTask':
    case 'manualTask':
      return formFieldNames(data);

    case 'callActivity': {
      const mapping = data.out_mapping ?? data.outMapping;
      if (mapping && typeof mapping === 'object') {
        return Object.keys(mapping as Record<string, unknown>);
      }
      return [];
    }

    default:
      // A gateway reads and chooses; it never writes. Worth knowing, and worth
      // the panel being able to say so.
      return [];
  }
}

/** The fields a form asks a person to fill in, which become variables. */
function formFieldNames(data: BPMNNodeData): string[] {
  const raw = data.formDefinition ?? (data.properties as Record<string, unknown> | undefined)?.form_definition;
  const parsed = typeof raw === 'string' ? safeParse(raw) : raw;
  if (!Array.isArray(parsed)) return [];
  return parsed
    .map((field) => (field && typeof field === 'object' ? asText((field as { id?: unknown }).id) : ''))
    .filter(Boolean);
}

function safeParse(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

/**
 * Walk the diagram and work out the bag at every step.
 *
 * Follows the arrows from each start event. A step is only computed once
 * everything leading to it has been, so what it sees is the whole of what came
 * before rather than whichever path was walked first. A cycle — a retry loop is
 * an ordinary way to draw one — is cut rather than followed forever.
 */
export function computeDataFlow(
  nodes: Node<BPMNNodeData>[],
  edges: Edge<BPMNEdgeData>[],
): Map<string, NodeDataFlow> {
  const sample = sampleDataOf(nodes);
  const byId = new Map(nodes.map((node) => [node.id, node]));
  const outgoing = new Map<string, string[]>();
  const incoming = new Map<string, string[]>();

  for (const edge of edges) {
    outgoing.set(edge.source, [...(outgoing.get(edge.source) ?? []), edge.target]);
    incoming.set(edge.target, [...(incoming.get(edge.target) ?? []), edge.source]);
  }

  const flow = new Map<string, NodeDataFlow>();
  const done = new Set<string>();

  const starts = nodes.filter((node) => (incoming.get(node.id) ?? []).length === 0);
  const queue = starts.map((node) => node.id);
  let guard = nodes.length * nodes.length + 16; // a cycle must not spin forever

  while (queue.length > 0 && guard-- > 0) {
    const id = queue.shift();
    if (!id) continue;
    const node = byId.get(id);
    if (!node) continue;

    const sources = incoming.get(id) ?? [];
    const ready = sources.every((source) => done.has(source) || !byId.has(source));
    if (!ready && sources.some((source) => !done.has(source))) {
      // Come back to it once the rest of what leads here has been worked out,
      // unless nothing ever will — a loop back to an earlier step.
      if (queue.length > 0) {
        queue.push(id);
        continue;
      }
    }

    const before = mergeIncoming(sources, flow, byId);
    const produces = producedNames(node, sample).map((name) => ({
      name,
      producedBy: asText(node.data.label, node.id),
      sample: sample[name],
      always: true,
    }));

    flow.set(id, { before, produces, after: mergeVariables([before, produces]) });
    done.add(id);

    for (const next of outgoing.get(id) ?? []) {
      if (!done.has(next)) queue.push(next);
    }
  }

  return flow;
}

/**
 * What is in the bag when several paths lead to the same step.
 *
 * A value set on only one branch is included but marked: it may not be there,
 * which is exactly what someone reading it needs to know.
 */
function mergeIncoming(
  sources: string[],
  flow: Map<string, NodeDataFlow>,
  byId: Map<string, Node<BPMNNodeData>>,
): Variable[] {
  const known = sources.filter((source) => byId.has(source)).map((source) => flow.get(source)?.after ?? []);
  if (known.length === 0) return [];

  const merged = mergeVariables(known);
  if (known.length === 1) return merged;

  return merged.map((variable) => ({
    ...variable,
    always: known.every((path) => path.some((candidate) => candidate.name === variable.name)),
  }));
}

/** Combine several bags, keeping the first mention of each name. */
function mergeVariables(groups: Variable[][]): Variable[] {
  const out = new Map<string, Variable>();
  for (const group of groups) {
    for (const variable of group) {
      const existing = out.get(variable.name);
      if (!existing) {
        out.set(variable.name, variable);
        continue;
      }
      // Mentioned by more than one path: it is only certain if every mention is.
      out.set(variable.name, { ...existing, always: existing.always && variable.always });
    }
  }
  return [...out.values()].sort((a, b) => a.name.localeCompare(b.name));
}
