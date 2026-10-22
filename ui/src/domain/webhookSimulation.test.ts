import { describe, expect, it } from 'bun:test';

import type { ApiWebhookSimulation } from '../services/domains/webhookService';
import { sampleBodyFor, simulationSteps, simulationVerdict } from './webhookSimulation';

const acting: ApiWebhookSimulation = {
  would_act: true,
  enabled: true,
  signature: { checked: true, valid: true },
  message_name: 'payment.received',
  correlation_expression: 'data.order_ref',
  correlation_key: 'PO-4182',
  waiting: [
    {
      instance_id: 'i-1',
      process_name: 'Purchase approval',
      business_key: 'PO-4182',
      node_id: 'wait',
      node_name: 'Wait for payment',
      since: '2026-10-21T09:00:00Z',
    },
  ],
  waiting_total: 1,
};

const titles = (result: ApiWebhookSimulation) => simulationSteps(result).map((step) => step.title);

describe('a simulated delivery, step by step', () => {
  it('walks a delivery that would act through each check in order', () => {
    expect(titles(acting)).toEqual([
      'Signature checks out',
      'Found which process this is about',
      '1 process is waiting for it',
    ]);
    expect(simulationSteps(acting)[1].detail).toBe('Your rule data.order_ref gives "PO-4182".');
    expect(simulationVerdict(acting)).toEqual({ tone: 'ok', text: 'A real delivery would be acted on' });
  });

  it('says an unsigned delivery would act only once it is signed', () => {
    const unsigned = { ...acting, signature: { checked: false, valid: false } };
    expect(simulationSteps(unsigned)[0]).toMatchObject({ tone: 'skip', title: 'Signature not checked' });
    expect(simulationSteps(unsigned)[0].detail).toContain('X-Metis-Signature');
    expect(simulationVerdict(unsigned).text).toBe('A real delivery would be acted on, once it is signed');
  });

  it("gives the server's reason for a signature it refused", () => {
    const problem = "The signature does not match this webhook's secret.";
    const refused = {
      ...acting,
      would_act: false,
      refusal: problem,
      signature: { checked: true, valid: false, problem },
    };
    expect(simulationSteps(refused)[0]).toEqual({
      key: 'signature',
      tone: 'bad',
      title: 'The signature is not accepted',
      detail: problem,
    });
    expect(simulationVerdict(refused)).toEqual({ tone: 'bad', text: 'A real delivery would be refused' });
  });

  it('stops at a body the rule finds nothing in, since nothing after it was looked at', () => {
    const problem = '"data.order_ref" found nothing in the delivery.';
    const unread = {
      ...acting,
      would_act: false,
      refusal: problem,
      body_problem: problem,
      correlation_key: undefined,
      waiting: [],
      waiting_total: 0,
    };
    expect(titles(unread)).toEqual(['Signature checks out', 'Could not tell which process this is about']);
  });

  it('puts a switched-off webhook first', () => {
    const off = { ...acting, enabled: false, would_act: false, refusal: 'This webhook is switched off.' };
    expect(simulationSteps(off)[0]).toMatchObject({ tone: 'bad', title: 'This webhook is switched off' });
  });

  it('warns of a delivery ID already received, and of nothing waiting', () => {
    const repeat = { ...acting, would_act: false, already_received: true };
    expect(titles(repeat)).toContain('This delivery ID was already received');
    expect(simulationVerdict(repeat)).toEqual({ tone: 'warn', text: 'A real delivery would change nothing' });

    const idle = { ...acting, would_act: false, waiting: [], waiting_total: 0 };
    expect(simulationSteps(idle).at(-1)).toMatchObject({
      tone: 'warn',
      title: 'Nothing is waiting for "PO-4182"',
    });
  });

  it('names what an uncorrelated webhook would start', () => {
    const starting = {
      ...acting,
      correlation_expression: undefined,
      correlation_key: undefined,
      waiting: [],
      waiting_total: 0,
      starts: [{ process_key: 'intake', process_name: 'Order intake' }, { process_key: 'audit' }],
    };
    expect(titles(starting)).toEqual(['Signature checks out', 'Read the delivery', 'It would start 2 processes']);
    expect(simulationSteps(starting).at(-1)?.detail).toBe('Order intake, audit');
  });
});

describe('the body a simulation starts from', () => {
  it("fills in the correlation rule's field", () => {
    expect(JSON.parse(sampleBodyFor('data.order_ref'))).toEqual({ data: { order_ref: 'ORD-1' } });
  });

  it('starts from an empty object when there is no plain path to fill', () => {
    expect(sampleBodyFor(undefined)).toBe('{}');
    expect(sampleBodyFor('string(order.id)')).toBe('{}');
  });
});
