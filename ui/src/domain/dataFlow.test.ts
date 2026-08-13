import { describe, expect, it } from 'bun:test';

import { computeDataFlow, sampleDataOf } from './dataFlow';

/**
 * The tracer answers "where did this value come from?" and "why is my variable
 * empty?" from the diagram alone. Both answers are only worth having if they
 * are right, and the second one — a value set on one branch and read after the
 * paths rejoin — is the case nobody notices while modelling.
 */

type TestNode = Parameters<typeof computeDataFlow>[0][number];
type TestEdge = Parameters<typeof computeDataFlow>[1][number];

function node(id: string, type: string, data: Record<string, unknown> = {}): TestNode {
  return { id, type, position: { x: 0, y: 0 }, data: { nodeType: type, label: id, ...data } } as TestNode;
}

function edge(source: string, target: string): TestEdge {
  return { id: `${source}->${target}`, source, target } as TestEdge;
}

const names = (variables: { name: string }[]) => variables.map((v) => v.name).sort();

describe('sample data on the start event', () => {
  it('is read from the start event', () => {
    const nodes = [node('start', 'startEvent', { sampleData: '{"amount":2400,"currency":"GBP"}' })];
    expect(sampleDataOf(nodes)).toEqual({ amount: 2400, currency: 'GBP' });
  });

  it('is ignored while it is still being typed', () => {
    const nodes = [node('start', 'startEvent', { sampleData: '{"amount": ' })];
    expect(sampleDataOf(nodes)).toEqual({});
  });

  it('is ignored when it is not an object', () => {
    const nodes = [node('start', 'startEvent', { sampleData: '[1,2,3]' })];
    expect(sampleDataOf(nodes)).toEqual({});
  });
});

describe('what each step receives and leaves', () => {
  it('carries the start data forward and adds what each step produces', () => {
    const nodes = [
      node('start', 'startEvent', { sampleData: '{"amount":2400}' }),
      node('decide', 'businessRuleTask', { output_mapping: { approvalLevel: 'approvalLevel' } }),
      node('end', 'endEvent'),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'decide'), edge('decide', 'end')]);

    expect(names(flow.get('decide')!.before)).toEqual(['amount']);
    expect(names(flow.get('decide')!.produces)).toEqual(['approvalLevel']);
    expect(names(flow.get('end')!.before)).toEqual(['amount', 'approvalLevel']);
  });

  it('says where a value came from', () => {
    const nodes = [
      node('start', 'startEvent', { label: 'Expense submitted', sampleData: '{"amount":2400}' }),
      node('end', 'endEvent'),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'end')]);

    expect(flow.get('end')!.before[0]).toMatchObject({
      name: 'amount',
      producedBy: 'Expense submitted',
      sample: 2400,
    });
  });

  it('marks a value that only one branch sets', () => {
    // start → route → (director | manager) → end. Only the director records a note.
    const nodes = [
      node('start', 'startEvent', { sampleData: '{"amount":2400}' }),
      node('route', 'exclusiveGateway'),
      node('director', 'userTask', {
        formDefinition: '[{"id":"approved"},{"id":"approverNote"}]',
      }),
      node('manager', 'userTask', { formDefinition: '[{"id":"approved"}]' }),
      node('end', 'endEvent'),
    ];
    const flow = computeDataFlow(nodes, [
      edge('start', 'route'),
      edge('route', 'director'),
      edge('route', 'manager'),
      edge('director', 'end'),
      edge('manager', 'end'),
    ]);

    const atEnd = flow.get('end')!.before;
    expect(atEnd.find((v) => v.name === 'approved')?.always).toBe(true);
    expect(atEnd.find((v) => v.name === 'approverNote')?.always).toBe(false);
  });

  it('says a gateway adds nothing', () => {
    const nodes = [
      node('start', 'startEvent', { sampleData: '{"amount":1}' }),
      node('route', 'exclusiveGateway'),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'route')]);
    expect(flow.get('route')!.produces).toEqual([]);
  });
});

describe('what a step is read to produce', () => {
  it('takes a service task’s output mappings, under the names they are stored as', () => {
    const nodes = [
      node('start', 'startEvent'),
      node('lookup', 'serviceTask', {
        properties: {
          http_url: 'https://example.invalid',
          input_companyNumber: 'registration_id',
          output_credit_score: 'creditScore',
          output_status: 'companyStatus',
        },
      }),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'lookup')]);

    // The names the process will read, not the endpoint's names, and nothing
    // for the input mapping — that is a value going out, not one arriving.
    expect(names(flow.get('lookup')!.produces)).toEqual(['companyStatus', 'creditScore']);
  });

  it('takes a form’s fields', () => {
    const nodes = [
      node('start', 'startEvent'),
      node('review', 'userTask', { formDefinition: '[{"id":"decision"},{"id":"reviewedBy"}]' }),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'review')]);
    expect(names(flow.get('review')!.produces)).toEqual(['decision', 'reviewedBy']);
  });

  it('takes a script task’s result variable', () => {
    const nodes = [node('start', 'startEvent'), node('calc', 'scriptTask', { resultVariable: 'total' })];
    const flow = computeDataFlow(nodes, [edge('start', 'calc')]);
    expect(names(flow.get('calc')!.produces)).toEqual(['total']);
  });

  it('produces nothing for a step that has not been configured yet', () => {
    const nodes = [node('start', 'startEvent'), node('lookup', 'serviceTask')];
    const flow = computeDataFlow(nodes, [edge('start', 'lookup')]);
    expect(flow.get('lookup')!.produces).toEqual([]);
  });
});

describe('diagrams that are still being drawn', () => {
  it('does not follow a loop forever', () => {
    // A retry loop is an ordinary thing to draw.
    const nodes = [
      node('start', 'startEvent', { sampleData: '{"amount":1}' }),
      node('a', 'serviceTask', { resultVariable: 'x' }),
      node('b', 'exclusiveGateway'),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'a'), edge('a', 'b'), edge('b', 'a')]);

    expect(flow.get('a')).toBeDefined();
    expect(flow.get('b')).toBeDefined();
  });

  it('leaves a step nothing leads to out of the trace', () => {
    const nodes = [
      node('start', 'startEvent', { sampleData: '{"amount":1}' }),
      node('end', 'endEvent'),
      node('orphan', 'userTask', { formDefinition: '[{"id":"note"}]' }),
    ];
    const flow = computeDataFlow(nodes, [edge('start', 'end')]);

    // It is reachable from nowhere, so it is treated as its own beginning
    // rather than being given data it will never see.
    expect(flow.get('orphan')!.before).toEqual([]);
  });

  it('handles an empty diagram', () => {
    expect(computeDataFlow([], []).size).toBe(0);
  });
});
