import { describe, expect, it } from 'bun:test';

import { editRawSettings, rawEditorView } from '../domain/rawSettings';
import { buildDefinitionPayload, mapLoadedEdges, mapLoadedNodes, restoredNodes } from './definitionMapper';

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

  /**
   * The arrow's caption is not executable.
   *
   * Labelling a path "Yes" used to deploy it with the condition `Yes` — an
   * unbound name, so the gateway found no matching flow, the path was never
   * taken, and (with the implicit-default flag off, which is the default) the
   * instance raised an incident at the first decision. Nothing in the designer
   * said so.
   */
  it('never turns a caption into a condition', () => {
    const payload = buildDefinitionPayload(
      'A process',
      'a-process',
      [] as Nodes,
      [{ id: 'f1', source: 'start', target: 'end', label: 'Yes' }] as Edges,
    );

    expect(payload.flows[0].condition).toBe('');
  });

  it('keeps the condition when a caption is also present', () => {
    const payload = buildDefinitionPayload(
      'A process',
      'a-process',
      [] as Nodes,
      [{ id: 'f1', source: 'start', target: 'end', label: 'Approved', data: { condition: 'amount > 1000' } }] as Edges,
    );

    expect(payload.flows[0].condition).toBe('amount > 1000');
  });
});

/**
 * Two service-task settings the engine reads under names the panel did not
 * write. The panel wrote `url` and `topic`; the engine reads the `http_url`
 * property and the external topic column, which the mapper fills from `httpUrl`
 * and `externalTopic`. So a "call a web address" step deployed and did nothing
 * at all, and the canvas showed no sign of it.
 */
describe('service task wiring', () => {
  it('sends the web address under the name the engine reads', () => {
    const saved = payloadFor({ httpUrl: 'https://api.example.com/hook' }, 'serviceTask');
    expect(saved.properties?.http_url).toBe('https://api.example.com/hook');
  });

  it('sends the external topic as its own field, not a setting', () => {
    const saved = payloadFor({ externalTopic: 'process-invoice' }, 'serviceTask');
    expect(saved.external_topic).toBe('process-invoice');
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

/**
 * The throwing events reach the engine under the names it actually reads.
 *
 * These were drawable nowhere until the palette gained them, so nothing had
 * ever checked that what the property panel writes arrives where the handler
 * looks. The handlers read `error_code` as a column, and `escalation_code`,
 * `activity_ref`, `signal_name`, `message_name` and `correlation_key` out of
 * the settings bag — a field saved under the editor's camelCase name would be
 * silently ignored, and the step would throw an empty code that every handler
 * catches.
 */
describe('what the throwing events save', () => {
  it('puts an error end event’s code in the column the engine reads', () => {
    const saved = payloadFor({ errorCode: 'PAYMENT_DECLINED' }, 'errorEndEvent');
    expect(saved.error_code).toBe('PAYMENT_DECLINED');
  });

  it('saves an escalation’s code where TriggerEscalation looks for it', () => {
    const saved = payloadFor({ escalationCode: 'NEEDS_MANAGER' }, 'escalationThrowEvent');
    expect(saved.properties?.escalation_code).toBe('NEEDS_MANAGER');
  });

  it('saves which step to undo where TriggerCompensation looks for it', () => {
    const saved = payloadFor({ activityRef: 'reserve_stock' }, 'compensationThrowEvent');
    expect(saved.properties?.activity_ref).toBe('reserve_stock');
  });

  it('saves a broadcast under signal_name', () => {
    const saved = payloadFor({ signalName: 'PAYMENT_CLEARED' }, 'intermediateThrowEvent');
    expect(saved.properties?.signal_name).toBe('PAYMENT_CLEARED');
  });

  it('saves a directed message with the value that picks one process', () => {
    const saved = payloadFor(
      { messageName: 'ORDER_READY', correlationKey: '${orderId}' },
      'intermediateThrowEvent',
    );
    expect(saved.properties?.message_name).toBe('ORDER_READY');
    expect(saved.properties?.correlation_key).toBe('${orderId}');
  });
});

/**
 * Opening an imported diagram and saving it without touching it.
 *
 * A BPMN file from another tool carries a layout: every shape's size, whether a
 * sub-process is drawn expanded, and the exact bends of every connector. None
 * of that is anything the designer draws with — React Flow sizes nodes by CSS
 * and routes its own edges — so it only survives if the mapper carries it
 * through untouched. Dropped, the symptom is the worst kind: the diagram opens
 * looking right, and is flattened to this tool's defaults by the one action a
 * user is certain changed nothing.
 */
describe('imported diagram geometry', () => {
  it('carries node size and expansion back out on save', () => {
    const loaded = mapLoadedNodes([
      // @ts-expect-error — the fixture is deliberately the server's shape.
      { id: 'sub', name: 'Review', type: 'subProcess', x: 100, y: 200, width: 350, height: 180, is_expanded: true },
    ]);

    expect(loaded[0].data.width).toBe(350);
    expect(loaded[0].data.height).toBe(180);
    expect(loaded[0].data.isExpanded).toBe(true);

    const payload = buildDefinitionPayload('P', 'p', loaded as Nodes, []);
    expect(payload.nodes[0].width).toBe(350);
    expect(payload.nodes[0].height).toBe(180);
    expect(payload.nodes[0].is_expanded).toBe(true);
  });

  it('does not write geometry into the property bag as well', () => {
    const loaded = mapLoadedNodes([
      // @ts-expect-error — the fixture is deliberately the server's shape.
      { id: 'sub', name: 'Review', type: 'subProcess', x: 1, y: 2, width: 350, height: 180, is_expanded: true },
    ]);
    const payload = buildDefinitionPayload('P', 'p', loaded as Nodes, []);

    expect(payload.nodes[0].properties).not.toHaveProperty('width');
    expect(payload.nodes[0].properties).not.toHaveProperty('height');
    expect(payload.nodes[0].properties).not.toHaveProperty('isExpanded');
  });

  it('keeps the bends an author put in a connector', () => {
    const edges = mapLoadedEdges([
      // @ts-expect-error — the fixture is deliberately the server's shape.
      { id: 'f1', source_ref: 'a', target_ref: 'b', waypoints: [{ x: 10, y: 20 }, { x: 30, y: 20 }, { x: 30, y: 60 }] },
    ]);

    const payload = buildDefinitionPayload('P', 'p', [], edges as Edges);
    expect(payload.flows[0].waypoints).toEqual([{ x: 10, y: 20 }, { x: 30, y: 20 }, { x: 30, y: 60 }]);
  });

  it('sends an empty route for an edge the designer drew', () => {
    const payload = buildDefinitionPayload('P', 'p', [], [
      { id: 'f1', source: 'a', target: 'b', data: { condition: '' } },
    ] as Edges);

    expect(payload.flows[0].waypoints).toEqual([]);
  });
});

type Loaded = ReturnType<typeof mapLoadedNodes>[number];

/** A step as the designer opens it from the server. */
function loaded(type: string, properties: Record<string, unknown>): Loaded {
  const [step] = mapLoadedNodes([
    { id: 'step', name: 'Step', type, x: 0, y: 0, properties },
  ] as Parameters<typeof mapLoadedNodes>[0]);
  return step;
}

/** The step's settings as the raw editor shows them. */
function shownIn(step: Loaded): Record<string, unknown> {
  return JSON.parse(rawEditorView(step.data, null).text) as Record<string, unknown>;
}

/** Edits the settings as shown, types the result into the raw editor, and merges it as the designer does. */
function afterRawEdit(step: Loaded, edit: (shown: Record<string, unknown>) => void): Loaded {
  const shown = shownIn(step);
  edit(shown);
  const { patch } = editRawSettings(step.data, JSON.stringify(shown, null, 2));
  return { ...step, data: { ...step.data, ...patch } };
}

function savedStep(step: Loaded) {
  return buildDefinitionPayload('A process', 'a-process', [step] as Nodes, [] as Edges).nodes[0];
}

/**
 * The raw editor shows a step's settings, and saving sends exactly those.
 *
 * A step opened from the server held each setting up to three times: under
 * its stored name, under the panel's camelCase name, and again in a nested
 * copy of everything the server sent, which saving started from. Deleting
 * http_url and httpUrl in the raw editor left the nested copy, so the address
 * was saved all the same. A rename left it too, under the old name.
 */
describe('what the raw editor shows is what is saved', () => {
  const address = { implementation: 'push', http_url: 'https://old.example/notify' };

  it('drops a loaded setting that is deleted in the raw editor', () => {
    const edited = afterRawEdit(loaded('serviceTask', address), (shown) => {
      delete shown.http_url;
      delete shown.httpUrl;
    });

    expect(savedStep(edited).properties).not.toHaveProperty('http_url');
  });

  it('saves a loaded setting renamed in the raw editor under its new name only', () => {
    const edited = afterRawEdit(loaded('serviceTask', address), (shown) => {
      shown.endpoint = shown.httpUrl ?? shown.http_url;
      delete shown.http_url;
      delete shown.httpUrl;
    });

    expect(savedStep(edited).properties).toMatchObject({ endpoint: 'https://old.example/notify' });
    expect(savedStep(edited).properties).not.toHaveProperty('http_url');
  });

  it('shows each loaded setting once', () => {
    // Every setting the loader gave a second name to, and some it did not,
    // each with a value found nowhere else.
    const stored = [
      'implementation', 'connector_instance_id', 'lock_duration', 'http_url', 'http_method', 'headers',
      'input_mapping', 'output_mapping', 'result_variable', 'event_type', 'timer_type', 'timer_duration',
      'signal_name', 'message_name', 'correlation_key', 'condition_expression', 'escalation_code',
      'activity_ref', 'non_interrupting', 'form_definition', 'decision_key', 'called_process_key', 'auth_token',
    ];
    const properties = Object.fromEntries(stored.map((key) => [key, `value-of-${key}`]));
    const text = rawEditorView(loaded('serviceTask', properties).data, null).text;

    for (const key of stored) {
      expect(text.split(`"value-of-${key}"`).length - 1, key).toBe(1);
    }
    expect(savedStep(loaded('serviceTask', properties)).properties).toEqual(properties);
  });
});

describe('a step opened from the server', () => {
  it('saves a decision table mapping changed in the panel', () => {
    // The panel writes the mapping under the name the server stores it by.
    // The loader had also put the old mapping under inputMapping, which
    // saving sends as input_mapping too, after it, so the edit was lost.
    const rule = loaded('businessRuleTask', { decision_key: 'risk', input_mapping: { score: 'creditScore' } });
    const edited = { ...rule, data: { ...rule.data, input_mapping: { score: 'riskScore' } } };

    expect(savedStep(edited).properties.input_mapping).toEqual({ score: 'riskScore' });
  });
});

describe('a draft kept in the browser by an earlier version', () => {
  // Opened when steps held a setting up to three times, then edited in the
  // panel: the panel's copy is the new one, and the others are stale.
  const draft = [{
    id: 'rule',
    type: 'businessRuleTask',
    position: { x: 0, y: 0 },
    data: {
      label: 'Rule',
      nodeType: 'businessRuleTask',
      decision_key: 'risk',
      input_mapping: { score: 'riskScore' },
      inputMapping: { score: 'creditScore' },
      http_url: 'https://old.example',
      httpUrl: 'https://new.example',
      properties: { decision_key: 'risk', input_mapping: { score: 'creditScore' }, deleted_since: 'gone' },
    },
  }] as Nodes;

  it('holds each setting once, as the panel last showed it', () => {
    const [restored] = restoredNodes(draft);

    expect(Object.keys(restored.data)).not.toEqual(expect.arrayContaining(['properties']));
    expect(Object.keys(restored.data)).not.toEqual(expect.arrayContaining(['inputMapping']));
    expect(Object.keys(restored.data)).not.toEqual(expect.arrayContaining(['http_url']));
  });

  it('saves what the panel showed, and nothing deleted since', () => {
    const saved = savedStep(restoredNodes(draft)[0]);

    expect(saved.properties).toMatchObject({ input_mapping: { score: 'riskScore' }, http_url: 'https://new.example' });
    expect(saved.properties).not.toHaveProperty('deleted_since');
  });
});
