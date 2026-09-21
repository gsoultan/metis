import { describe, expect, it } from 'bun:test';
import { describeRole, readIntegrationSurface } from './sdkSurface';
import type { ApiNode } from '../services/types';

function node(partial: Partial<ApiNode> & { id: string; type: string }): ApiNode {
  return { name: '', x: 0, y: 0, ...partial } as ApiNode;
}

describe('readIntegrationSurface', () => {
  it('lists a service task topic as work your code pulls', () => {
    const surface = readIntegrationSurface([
      node({ id: 'charge', name: 'Reverse the charge', type: 'serviceTask', external_topic: 'reverse-charge' }),
    ]);

    expect(surface.topics).toEqual([
      { nodeId: 'charge', nodeName: 'Reverse the charge', nodeType: 'serviceTask', name: 'reverse-charge', role: 'you-act' },
    ]);
    expect(surface.isEmpty).toBe(false);
  });

  it('lists a shared topic once, because one worker serves both nodes', () => {
    const surface = readIntegrationSurface([
      node({ id: 'a', type: 'serviceTask', external_topic: 'notify' }),
      node({ id: 'b', type: 'serviceTask', external_topic: 'notify' }),
    ]);

    expect(surface.topics.map((t) => t.name)).toEqual(['notify']);
    expect(surface.topics[0].nodeId).toBe('a');
  });

  it('trims the topic, so a trailing space is not a second integration', () => {
    const surface = readIntegrationSurface([node({ id: 'a', type: 'serviceTask', external_topic: ' notify ' })]);
    expect(surface.topics[0].name).toBe('notify');
  });

  it('separates a message the process waits for from one it throws', () => {
    const surface = readIntegrationSurface([
      node({ id: 'wait', type: 'intermediateCatchEvent', properties: { message_name: 'payment.received', correlation_key: 'orderId' } }),
      node({ id: 'announce', type: 'intermediateThrowEvent', properties: { message_name: 'refund.issued' } }),
    ]);

    expect(surface.inbound.map((p) => p.name)).toEqual(['payment.received']);
    expect(surface.inbound[0].role).toBe('you-notify');
    expect(surface.inbound[0].correlationKey).toBe('orderId');
    expect(surface.outbound.map((p) => p.name)).toEqual(['refund.issued']);
    expect(surface.outbound[0].role).toBe('you-listen');
  });

  it('carries no correlation key for a signal, which is broadcast to everyone', () => {
    const surface = readIntegrationSurface([
      node({ id: 'q', type: 'intermediateCatchEvent', properties: { signal_name: 'quarter.closed', correlation_key: 'ignored' } }),
    ]);

    expect(surface.inbound[0].correlationKey).toBeUndefined();
  });

  it('counts a boundary event as something you notify, not something you hear', () => {
    const surface = readIntegrationSurface([
      node({ id: 'cancel', type: 'boundaryEvent', attached_to_ref: 'task', properties: { message_name: 'order.cancelled' } }),
    ]);

    expect(surface.inbound.map((p) => p.nodeId)).toEqual(['cancel']);
    expect(surface.outbound).toEqual([]);
  });

  it('names a node by its id when the model left the name blank', () => {
    const surface = readIntegrationSurface([node({ id: 'Task_0x91', name: '   ', type: 'userTask' })]);
    expect(surface.humanSteps[0].nodeName).toBe('Task_0x91');
  });

  it('reports a model with no outside contract as empty rather than as an error', () => {
    const surface = readIntegrationSurface([
      node({ id: 'start', type: 'startEvent' }),
      node({ id: 'gate', type: 'exclusiveGateway' }),
      node({ id: 'end', type: 'endEvent' }),
    ]);

    expect(surface.isEmpty).toBe(true);
  });

  it('prefers the topic over anything else on the same node, since that is what the engine dispatches', () => {
    const surface = readIntegrationSurface([
      node({ id: 'both', type: 'serviceTask', external_topic: 'pull-me', properties: { message_name: 'ignored' } }),
    ]);

    expect(surface.topics.map((t) => t.name)).toEqual(['pull-me']);
    expect(surface.outbound).toEqual([]);
  });
});

describe('describeRole', () => {
  it('says the pull model out loud, so nobody builds a webhook receiver for a topic', () => {
    expect(describeRole('you-act')).toContain('pulls it');
  });
});
