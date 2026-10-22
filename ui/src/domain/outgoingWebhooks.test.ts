import { describe, expect, it } from 'bun:test';

import type { ApiOutgoingDelivery } from '../services/domains/outgoingWebhookService';
import {
  EVENT_GROUPS,
  deliveryActions,
  deliveryOutcome,
  deliverySubject,
  eventLabel,
  nextTryLabel,
  triesLabel,
  troubleHint,
} from './outgoingWebhooks';

const delivery = (over: Partial<ApiOutgoingDelivery>): ApiOutgoingDelivery => ({
  id: 'd-1',
  webhook_id: 'o-1',
  event: 'task.completed',
  status: 'pending',
  attempts: 0,
  body: {},
  created_at: '2026-10-21T10:41:00Z',
  ...over,
});

describe('the events a webhook can be sent', () => {
  it('offers every event the server knows, once', () => {
    const offered = EVENT_GROUPS.flatMap((group) => group.choices.map((choice) => choice.event));
    expect([...offered].sort()).toEqual(['incident.raised', 'process.completed', 'process.started', 'task.completed', 'task.created']);
  });

  it('keeps the name of an event it has no words for', () => {
    expect(eventLabel('task.completed')).toBe('Task completed');
    expect(eventLabel('invoice.paid')).toBe('invoice.paid');
  });
});

describe('where a delivery stands', () => {
  it('says what the last try was answered and what happens next', () => {
    expect(deliveryOutcome(delivery({ attempts: 4, last_status_code: 503 }))).toEqual({ tone: 'retrying', label: '503 · retrying' });
    expect(deliveryOutcome(delivery({ attempts: 2 }))).toEqual({ tone: 'retrying', label: 'No answer · retrying' });
    expect(deliveryOutcome(delivery({ attempts: 0 }))).toEqual({ tone: 'retrying', label: 'Waiting to send' });
    expect(deliveryOutcome(delivery({ status: 'delivered', attempts: 1, last_status_code: 200 }))).toEqual({ tone: 'delivered', label: '200 · delivered' });
    expect(deliveryOutcome(delivery({ status: 'failed', attempts: 10, last_status_code: 401 }))).toEqual({ tone: 'failed', label: '401 · gave up' });
  });

  it('does not call a refused test a delivery that gave up', () => {
    expect(deliveryOutcome(delivery({ status: 'failed', attempts: 1, last_status_code: 418, test: true })).label).toBe('418 · test refused');
  });

  it('counts tries against the limit only while there are more to come', () => {
    expect(triesLabel(delivery({ attempts: 4 }))).toBe('4 of 10');
    expect(triesLabel(delivery({ status: 'failed', attempts: 10 }))).toBe('10');
    expect(triesLabel(delivery({ status: 'delivered', attempts: 1, test: true }))).toBe('1');
  });

  it('says when the next try is, and nothing once there is none', () => {
    const now = new Date('2026-10-21T10:00:00Z');
    expect(nextTryLabel(delivery({ next_attempt_at: '2026-10-21T10:08:00Z' }), now)).toBe('in 8 min');
    expect(nextTryLabel(delivery({ next_attempt_at: '2026-10-21T12:00:00Z' }), now)).toBe('in 2 h');
    expect(nextTryLabel(delivery({ next_attempt_at: '2026-10-21T09:59:00Z' }), now)).toBe('now');
    expect(nextTryLabel(delivery({ status: 'failed', next_attempt_at: '2026-10-21T10:08:00Z' }), now)).toBe('–');
    expect(nextTryLabel(delivery({ next_attempt_at: 'soon' }), now)).toBe('–');
  });

  it('offers what the server allows: retry what is not delivered, stop what is pending, nothing on a test', () => {
    expect(deliveryActions(delivery({}))).toEqual({ retry: true, stop: true });
    expect(deliveryActions(delivery({ status: 'failed' }))).toEqual({ retry: true, stop: false });
    expect(deliveryActions(delivery({ status: 'delivered' }))).toEqual({ retry: false, stop: false });
    expect(deliveryActions(delivery({ status: 'failed', test: true }))).toEqual({ retry: false, stop: false });
  });
});

describe('the case a delivery is about', () => {
  it('is named by its process and business key', () => {
    expect(deliverySubject({ instance: { process_name: 'Purchase approval', business_key: 'PO-4182' } })).toBe('Purchase approval · PO-4182');
    expect(deliverySubject({ instance: { process_key: 'purchase' } })).toBe('purchase');
  });

  it('says nothing for a body not shaped like a delivery', () => {
    expect(deliverySubject(null)).toBe('');
    expect(deliverySubject('text')).toBe('');
    expect(deliverySubject({ instance: { business_key: 42 } })).toBe('');
  });
});

describe('the next step after a failure', () => {
  it('points at the secret when the receiver refuses the signature', () => {
    expect(troubleHint(401, undefined)).toContain('signing secret');
  });

  it('points at the administrator when this installation will not call the address', () => {
    const error = 'this installation does not call that address: it resolves inside a private network, or is not on the list of hosts it may call';
    expect(troubleHint(undefined, error)).toContain('administrator');
  });

  it('has nothing to add to an answer it has no advice for', () => {
    expect(troubleHint(422, undefined)).toBeNull();
    expect(troubleHint(undefined, undefined)).toBeNull();
  });
});
