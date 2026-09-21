/**
 * What a process expects from the code outside it.
 *
 * A BPMN model is also an integration contract, and today that contract is only
 * readable by opening the model and clicking each node: the topic a service task
 * publishes, the message a catch event waits for, the signal a boundary event
 * listens to. Somebody writing a worker against it has to reverse-engineer the
 * diagram, guess the strings, and find out they guessed wrong at runtime — the
 * topic is matched exactly, so `reverse-charge` and `reverseCharge` are two
 * different integrations and neither reports the mismatch.
 *
 * This reads the contract out of the definition instead. The names here are the
 * ones the engine matches on, taken from the same fields the engine reads, so a
 * snippet generated from them cannot disagree with the model.
 */
import type { ApiNode } from '../services/types';

/** Which side of the boundary does the work. */
export type SurfaceRole =
  /** Your code does this step and reports back. */
  | 'you-act'
  /** The process is waiting; your code tells it something happened. */
  | 'you-notify'
  /** The process emits this; your code hears about it (webhook, event stream). */
  | 'you-listen'
  /** A person does it, in Metis's inbox or in your own UI over the task API. */
  | 'a-person-acts';

export interface SurfacePoint {
  /** Stable within one definition — the node is what makes it unique. */
  nodeId: string;
  nodeName: string;
  nodeType: string;
  /** The exact string the engine matches on. Empty for a user task. */
  name: string;
  role: SurfaceRole;
  /** Which instance the message is for, when the model names the variable. */
  correlationKey?: string;
}

export interface IntegrationSurface {
  /** Topics your workers pull from, deduplicated — several nodes may share one. */
  topics: SurfacePoint[];
  /** Messages and signals the process waits for. */
  inbound: SurfacePoint[];
  /** Messages and signals the process throws. */
  outbound: SurfacePoint[];
  /** Steps a person completes. */
  humanSteps: SurfacePoint[];
  /** True when nothing in the model reaches outside the engine. */
  isEmpty: boolean;
}

/** Node types that *wait* for a message or signal rather than emitting one. */
const CATCHING = new Set([
  'startEvent',
  'intermediateCatchEvent',
  'boundaryEvent',
  'eventBasedGateway',
  'messageEvent',
  'signalEvent',
]);

const HUMAN = new Set(['userTask', 'manualTask']);

function readProperty(node: ApiNode, key: string): string {
  const value = node.properties?.[key];
  return typeof value === 'string' ? value.trim() : '';
}

/**
 * Reads the integration points out of a definition's nodes.
 *
 * Order is the node order the server returned rather than anything sorted:
 * definitions come back in the order they were authored, which tracks the
 * reading order of the diagram more closely than an alphabetical list would.
 */
export function readIntegrationSurface(nodes: readonly ApiNode[]): IntegrationSurface {
  const topics: SurfacePoint[] = [];
  const inbound: SurfacePoint[] = [];
  const outbound: SurfacePoint[] = [];
  const humanSteps: SurfacePoint[] = [];
  const seenTopics = new Set<string>();

  for (const node of nodes) {
    const base = {
      nodeId: node.id,
      nodeName: node.name?.trim() || node.id,
      nodeType: node.type,
    };

    const topic = node.external_topic?.trim() ?? '';
    if (topic !== '') {
      // Two service tasks may publish the same topic on purpose — one worker
      // serving both is the normal arrangement — so the topic is listed once
      // and the first node that publishes it is the one named.
      if (!seenTopics.has(topic)) {
        seenTopics.add(topic);
        topics.push({ ...base, name: topic, role: 'you-act' });
      }
      continue;
    }

    if (HUMAN.has(node.type)) {
      humanSteps.push({ ...base, name: '', role: 'a-person-acts' });
      continue;
    }

    const message = readProperty(node, 'message_name');
    const signal = readProperty(node, 'signal_name');
    if (message === '' && signal === '') {
      continue;
    }

    const catching = CATCHING.has(node.type);
    const point: SurfacePoint = {
      ...base,
      name: message !== '' ? message : signal,
      role: catching ? 'you-notify' : 'you-listen',
    };
    const correlationKey = readProperty(node, 'correlation_key');
    if (message !== '' && correlationKey !== '') {
      point.correlationKey = correlationKey;
    }

    (catching ? inbound : outbound).push(point);
  }

  return {
    topics,
    inbound,
    outbound,
    humanSteps,
    isEmpty:
      topics.length === 0 && inbound.length === 0 && outbound.length === 0 && humanSteps.length === 0,
  };
}

/** Whether a surface point names a message (rather than a signal). */
export function isMessage(point: SurfacePoint): boolean {
  return point.correlationKey !== undefined || point.nodeType === 'messageEvent';
}

/**
 * One sentence saying what the integrator has to build for this point.
 *
 * Written here rather than in the component because it is decidable from the
 * point alone, and because the wording is the product: "Metis publishes this
 * step; your worker pulls it" is the sentence that stops somebody writing a
 * webhook receiver for a pull-model topic.
 */
export function describeRole(role: SurfaceRole): string {
  switch (role) {
    case 'you-act':
      return 'Metis publishes this step and waits. Your worker pulls it, does the work, and reports back.';
    case 'you-notify':
      return 'The process pauses here until you tell it this happened.';
    case 'you-listen':
      return 'The process announces this. Subscribe with a webhook if your side needs to know.';
    case 'a-person-acts':
      return 'A person completes this — in Metis’s inbox, or in your own UI over the task API.';
  }
}
