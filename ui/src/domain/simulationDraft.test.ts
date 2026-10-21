import { describe, expect, it } from 'bun:test';

import type { CreateDefinitionPayload, CreateNodePayload } from '../services/types';
import { definitionFingerprint, toDraftDefinition } from './simulationDraft';

function node(overrides: Partial<CreateNodePayload>): CreateNodePayload {
  return {
    id: 'approve',
    name: 'Approve the expense',
    type: 'userTask',
    x: 100,
    y: 80,
    width: 0,
    height: 0,
    is_expanded: false,
    assignee: '',
    candidate_users: [],
    candidate_groups: [],
    priority: 0,
    due_date: '',
    form_key: '',
    default_flow: '',
    script: '',
    script_format: '',
    external_topic: '',
    documentation: '',
    attached_to_ref: '',
    parent_id: '',
    cancel_activity: false,
    error_code: '',
    multi_instance_type: '',
    loop_cardinality: 0,
    collection: '',
    element_variable: '',
    completion_condition: '',
    is_event_sub_process: false,
    condition: '',
    properties: {},
    ...overrides,
  };
}

function payload(nodes: CreateNodePayload[]): CreateDefinitionPayload {
  return {
    key: 'expense-approval',
    name: 'Expense approval',
    nodes,
    flows: [{ id: 'f1', source_ref: 'start', target_ref: 'approve', condition: '', documentation: '' }],
  };
}

describe('toDraftDefinition', () => {
  it('names people and groups the way the server reads them', () => {
    const draft = toDraftDefinition(
      payload([node({ candidate_users: ['sarah'], candidate_groups: ['finance'] })]),
    );
    expect(draft.nodes[0].candidate_users).toEqual([{ username: 'sarah' }]);
    expect(draft.nodes[0].candidate_groups).toEqual([{ name: 'finance' }]);
  });

  it('rounds sizes, because a fraction makes the server refuse the whole request', () => {
    const draft = toDraftDefinition(payload([node({ width: 120.6, height: 79.2 })]));
    expect(draft.nodes[0].width).toBe(121);
    expect(draft.nodes[0].height).toBe(79);
  });
});

describe('definitionFingerprint', () => {
  it('ignores where a shape sits', () => {
    const before = payload([node({})]);
    const moved = payload([node({ x: 400, y: 320, width: 200, is_expanded: true })]);
    expect(definitionFingerprint(moved)).toBe(definitionFingerprint(before));
  });

  it('notices a change in what a step does', () => {
    const before = payload([node({})]);
    const changed = payload([node({ assignee: 'marc' })]);
    expect(definitionFingerprint(changed)).not.toBe(definitionFingerprint(before));
  });

  it('notices a changed condition on a flow', () => {
    const before = payload([node({})]);
    const changed = { ...before, flows: [{ ...before.flows[0], condition: 'amount > 1000' }] };
    expect(definitionFingerprint(changed)).not.toBe(definitionFingerprint(before));
  });

  it('does not care what order properties were written in', () => {
    const a = payload([node({ properties: { decision_key: 'limits', http_url: 'https://x' } })]);
    const b = payload([node({ properties: { http_url: 'https://x', decision_key: 'limits' } })]);
    expect(definitionFingerprint(a)).toBe(definitionFingerprint(b));
  });
});
