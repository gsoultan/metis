import { describe, expect, it } from 'bun:test';

import { buildDefinitionPayload, mapLoadedEdges, mapLoadedNodes } from './definitionMapper';

/**
 * Saving a process, and opening it again.
 *
 * This mapper spent some time silently discarding most of what the property
 * panel configured: it built a node's settings from a hand-written list, and
 * the editors had drifted off it. Choosing a decision for a business rule task
 * did nothing, and so did the connector on a service task, the called process
 * on a call activity, and every input and output mapping. The panel showed
 * them, they survived a reload, and none of them were ever sent.
 *
 * The rule is now inverted — anything on a node is a setting unless it is the
 * designer's own business — so these tests are mostly about that rule holding.
 */

type Nodes = Parameters<typeof buildDefinitionPayload>[2];
type Edges = Parameters<typeof buildDefinitionPayload>[3];

function node(id: string, type: string, data: Record<string, unknown>) {
  return { id, type, position: { x: 10, y: 20 }, data: { nodeType: type, label: id, ...data } };
}

function payloadFor(data: Record<string, unknown>, type = 'businessRuleTask') {
  const payload = buildDefinitionPayload('A process', 'a-process', [node('n1', type, data)] as Nodes, [] as Edges);
  return payload.nodes[0];
}

describe('what gets saved', () => {
  it('keeps the settings the property panel writes', () => {
    const saved = payloadFor({
      decision_key: 'expense-approval-level',
      decision_version: 2,
      input_mapping: { amount: 'amount' },
      output_mapping: { approvalLevel: 'approvalLevel' },
    });

    expect(saved.properties).toMatchObject({
      decision_key: 'expense-approval-level',
      decision_version: 2,
      input_mapping: { amount: 'amount' },
      output_mapping: { approvalLevel: 'approvalLevel' },
    });
  });

  it('keeps a setting nobody has taught it about', () => {
    // The point of the rule: a field added to an editor is saved without
    // anyone remembering to extend a list here.
    const saved = payloadFor({ some_new_setting: 'a value' });
    expect(saved.properties.some_new_setting).toBe('a value');
  });

  it('stores a camelCase editor field under the name the server uses', () => {
    const saved = payloadFor({ httpUrl: 'https://example.invalid', httpMethod: 'POST' }, 'serviceTask');
    expect(saved.properties.http_url).toBe('https://example.invalid');
    expect(saved.properties.http_method).toBe('POST');
  });

  it('does not put the designer’s own business in the settings', () => {
    const saved = payloadFor({ status: 'active', heatmapValue: 12, decision_key: 'k' });

    expect(saved.properties.status).toBeUndefined();
    expect(saved.properties.heatmapValue).toBeUndefined();
    expect(saved.properties.nodeType).toBeUndefined();
    expect(saved.properties.label).toBeUndefined();
    expect(saved.properties.decision_key).toBe('k');
  });

  it('puts the fields with a column of their own in that column', () => {
    const saved = payloadFor({ assignee: 'carol', priority: 3, dueDate: '2026-03-01' }, 'userTask');

    expect(saved.assignee).toBe('carol');
    expect(saved.priority).toBe(3);
    expect(saved.due_date).toBe('2026-03-01');
    // and not also in the free-form bag, where nothing reads them
    expect(saved.properties.assignee).toBeUndefined();
    expect(saved.properties.priority).toBeUndefined();
  });

  it('keeps the position, so a process reopens as it was drawn', () => {
    const saved = payloadFor({});
    expect(saved.x).toBe(10);
    expect(saved.y).toBe(20);
  });
});

describe('what gets read back', () => {
  it('gives the editors the settings under the names they read', () => {
    const saved = payloadFor({
      decision_key: 'expense-approval-level',
      input_mapping: { amount: 'amount' },
    });

    const [reopened] = mapLoadedNodes([
      { id: saved.id, name: saved.name, type: saved.type as string, x: saved.x, y: saved.y, properties: saved.properties },
    ] as Parameters<typeof mapLoadedNodes>[0]);

    // Read back blank, this looks unset — which invites setting it again, and
    // that new value used to be the one that got dropped.
    expect(reopened.data.decision_key).toBe('expense-approval-level');
    expect(reopened.data.input_mapping).toEqual({ amount: 'amount' });
  });

  it('survives a full round trip unchanged', () => {
    const settings = {
      decision_key: 'supplier-risk',
      decision_version: 3,
      input_mapping: { creditScore: 'creditScore' },
      output_mapping: { riskBand: 'riskBand' },
    };

    const once = payloadFor(settings);
    const [reopened] = mapLoadedNodes([
      { id: once.id, name: once.name, type: once.type as string, x: once.x, y: once.y, properties: once.properties },
    ] as Parameters<typeof mapLoadedNodes>[0]);

    const twice = buildDefinitionPayload('A process', 'a-process', [reopened] as Nodes, [] as Edges).nodes[0];
    expect(twice.properties).toMatchObject(settings);
  });
});

describe('the arrows between steps', () => {
  it('names both ends the way the server does', () => {
    const payload = buildDefinitionPayload(
      'A process',
      'a-process',
      [] as Nodes,
      [{ id: 'f1', source: 'start', target: 'end', data: { condition: 'approvalLevel = director' } }] as Edges,
    );

    expect(payload.flows[0]).toMatchObject({
      id: 'f1',
      source_ref: 'start',
      target_ref: 'end',
      condition: 'approvalLevel = director',
    });
  });

  it('reads them back into the ends the canvas draws from', () => {
    const [edge] = mapLoadedEdges([
      { id: 'f1', source_ref: 'start', target_ref: 'end', condition: 'approvalLevel = director' },
    ]);

    expect(edge.source).toBe('start');
    expect(edge.target).toBe('end');
    expect(edge.data?.condition).toBe('approvalLevel = director');
  });
});

/**
 * Boundary events, which the designer could not draw.
 *
 * The engine runs error, escalation and compensation boundary events, and
 * non-interrupting ones, and has tests for all of them. The property panel
 * offered only timer, message and signal, and the mapper carried no field for
 * the rest — so there was no way to author one short of editing the API by
 * hand.
 */
describe('boundary events', () => {
  it('saves which failure an error boundary catches', () => {
    const saved = payloadFor({ eventType: 'error', errorCode: 'payment-declined' }, 'boundaryEvent');

    // error_code is a field on the node, not a setting in the bag: the engine
    // matches on Node.ErrorCode.
    expect(saved.error_code).toBe('payment-declined');
  });

  it('treats a blank failure code as catching anything', () => {
    const saved = payloadFor({ eventType: 'error' }, 'boundaryEvent');
    expect(saved.error_code).toBe('');
  });

  it('saves the code an escalation is raised under', () => {
    const saved = payloadFor({ eventType: 'escalation', escalationCode: 'over-approval-limit' }, 'boundaryEvent');
    expect(saved.properties).toMatchObject({ escalation_code: 'over-approval-limit' });
  });

  it('saves a compensation boundary so the engine recognises it', () => {
    const saved = payloadFor({ eventType: 'compensation' }, 'boundaryEvent');
    expect(saved.properties).toMatchObject({ event_type: 'compensation' });
  });

  it('saves letting the attached step carry on', () => {
    const saved = payloadFor({ eventType: 'timer', duration: 'R3/PT1H', nonInterrupting: true }, 'boundaryEvent');

    expect(saved.properties).toMatchObject({
      non_interrupting: true,
      timer_duration: 'R3/PT1H',
    });
  });

  it('leaves a boundary interrupting unless it is asked not to', () => {
    const saved = payloadFor({ eventType: 'timer', duration: 'PT2H' }, 'boundaryEvent');

    // Absent rather than false: the engine reads an absent value as
    // interrupting, which is the BPMN default and what every stored definition
    // already does.
    expect(saved.properties?.non_interrupting).toBeUndefined();
  });

  it('reads all of it back when the process is opened again', () => {
    const [loaded] = mapLoadedNodes([
      {
        id: 'deadline',
        type: 'boundaryEvent',
        x: 0,
        y: 0,
        attached_to_ref: 'approve',
        error_code: 'payment-declined',
        properties: {
          event_type: 'error',
          escalation_code: 'over-approval-limit',
          activity_ref: 'book-flight',
          non_interrupting: true,
        },
      },
    ] as Parameters<typeof mapLoadedNodes>[0]);

    expect(loaded.data).toMatchObject({
      errorCode: 'payment-declined',
      escalationCode: 'over-approval-limit',
      activityRef: 'book-flight',
      nonInterrupting: true,
    });
  });
});
