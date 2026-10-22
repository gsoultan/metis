/**
 * What the outgoing webhook screens say, decided here where it can be tested.
 *
 * A receiver's events have names meant for the developer at the other end
 * (`task.completed`); whoever sets one up chooses them by what happens in
 * their processes. A delivery's state is three words on the server; on the
 * screen it is what came of the last try and what happens next.
 */
import type { ApiOutgoingDelivery, OutgoingWebhookEvent } from '../services/domains/outgoingWebhookService';

export interface EventChoice {
  event: OutgoingWebhookEvent;
  /** What happens, as whoever picks it thinks of it. */
  label: string;
  /** Short, for a badge in the list. */
  short: string;
}

export interface EventGroup {
  title: string;
  choices: EventChoice[];
}

/** Every event a webhook can be sent, grouped as the form lists them. */
export const EVENT_GROUPS: EventGroup[] = [
  {
    title: 'Processes',
    choices: [
      { event: 'process.started', label: 'A process starts', short: 'Process started' },
      { event: 'process.completed', label: 'A process finishes', short: 'Process completed' },
    ],
  },
  {
    title: 'Tasks',
    choices: [
      { event: 'task.created', label: 'A task is waiting for someone', short: 'Task created' },
      { event: 'task.completed', label: 'A task is completed', short: 'Task completed' },
    ],
  },
  {
    title: 'Problems',
    choices: [
      {
        event: 'incident.raised',
        label: 'A process gets stuck and needs attention (incident)',
        short: 'Incident raised',
      },
    ],
  },
];

const CHOICES = new Map<string, EventChoice>(EVENT_GROUPS.flatMap((group) => group.choices).map((c) => [c.event, c]));

/** The badge an event is shown as. An event this screen does not know keeps its own name. */
export function eventLabel(event: string): string {
  return CHOICES.get(event)?.short ?? event;
}

/** How many attempts a delivery gets before it is failed for good. */
export const MAX_ATTEMPTS = 10;

export type DeliveryTone = 'delivered' | 'retrying' | 'failed' | 'test';

export interface DeliveryOutcome {
  tone: DeliveryTone;
  /** "503 · retrying", "200 · delivered", "No answer · gave up". */
  label: string;
}

/** What the last try was answered with, in a few words. */
function lastAnswer(delivery: Pick<ApiOutgoingDelivery, 'last_status_code' | 'attempts'>): string {
  if (delivery.last_status_code) return String(delivery.last_status_code);
  return delivery.attempts > 0 ? 'No answer' : 'Not sent yet';
}

/** Where a delivery stands, as the log's Result column says it. */
export function deliveryOutcome(delivery: ApiOutgoingDelivery): DeliveryOutcome {
  const answer = lastAnswer(delivery);
  if (delivery.status === 'delivered') {
    return { tone: delivery.test ? 'test' : 'delivered', label: `${answer} · delivered` };
  }
  if (delivery.status === 'failed') {
    return { tone: 'failed', label: delivery.test ? `${answer} · test refused` : `${answer} · gave up` };
  }
  return { tone: 'retrying', label: delivery.attempts === 0 ? 'Waiting to send' : `${answer} · retrying` };
}

/** The Tries column: "4 of 10" while it may be tried again, the count once it is settled. */
export function triesLabel(delivery: Pick<ApiOutgoingDelivery, 'attempts' | 'status' | 'test'>): string {
  if (delivery.status === 'pending' && !delivery.test) return `${delivery.attempts} of ${MAX_ATTEMPTS}`;
  return String(delivery.attempts);
}

/**
 * When the next try is, in words: "in 8 min", "now".
 *
 * `now` is passed rather than read so a test can say what time it is.
 */
export function nextTryLabel(delivery: Pick<ApiOutgoingDelivery, 'next_attempt_at' | 'status'>, now: Date = new Date()): string {
  if (delivery.status !== 'pending' || !delivery.next_attempt_at) return '–';
  const due = new Date(delivery.next_attempt_at).getTime();
  if (Number.isNaN(due)) return '–';
  const minutes = Math.round((due - now.getTime()) / 60_000);
  if (minutes <= 0) return 'now';
  if (minutes < 60) return `in ${minutes} min`;
  const hours = Math.round(minutes / 60);
  return `in ${hours} h`;
}

/** Which actions the log offers for a delivery: the same ones the server allows. */
export function deliveryActions(delivery: Pick<ApiOutgoingDelivery, 'status' | 'test'>): { retry: boolean; stop: boolean } {
  if (delivery.test) return { retry: false, stop: false };
  return { retry: delivery.status !== 'delivered', stop: delivery.status === 'pending' };
}

/**
 * The case a delivery is about, in a few words: "Purchase approval · PO-4182".
 *
 * Read out of the body that was posted, which names the case and never its
 * data. A body that is not shaped as expected says nothing rather than guess.
 */
export function deliverySubject(body: unknown): string {
  if (!body || typeof body !== 'object') return '';
  const instance = (body as { instance?: { process_name?: unknown; process_key?: unknown; business_key?: unknown } })
    .instance;
  if (!instance) return '';
  const process = typeof instance.process_name === 'string' && instance.process_name !== ''
    ? instance.process_name
    : typeof instance.process_key === 'string'
      ? instance.process_key
      : '';
  const key = typeof instance.business_key === 'string' ? instance.business_key : '';
  return [process, key].filter((part) => part !== '').join(' · ');
}

/**
 * Why a delivery or a test did not get through, in words somebody can act on.
 *
 * The server's own words are kept: they already say which address was refused
 * and why. What is added is the next step, for the answers that have an
 * obvious one.
 */
export function troubleHint(statusCode: number | undefined, error: string | undefined): string | null {
  if (statusCode === 401 || statusCode === 403) {
    return 'The receiver refused the signature or the credentials. Check that it has this webhook’s signing secret.';
  }
  if (statusCode === 404 || statusCode === 410) {
    return 'Nothing answers at that address any more. Check the URL with whoever runs the receiver.';
  }
  if (statusCode !== undefined && statusCode >= 300 && statusCode < 400) {
    return 'The receiver answered with a redirect, which is never followed. Register the address it redirects to.';
  }
  if (statusCode !== undefined && statusCode >= 500) {
    return 'The receiver had a problem of its own. Metis keeps trying, so this usually clears up by itself.';
  }
  if (!statusCode && error) {
    if (/did not answer in time/i.test(error)) {
      return 'If the receiver does slow work before answering, have it answer first and do the work afterwards.';
    }
    if (/does not call that address/i.test(error)) {
      return 'If this address is meant to be reachable, ask your administrator to allow it.';
    }
  }
  return null;
}

/** A body for reading: indented JSON, or the text as it came. */
export function prettyBody(body: unknown): string {
  if (body === undefined || body === null) return '';
  if (typeof body === 'string') return body;
  try {
    return JSON.stringify(body, null, 2);
  } catch {
    return String(body);
  }
}
