import { describe, expect, it } from 'bun:test';

import {
  hasBlockingIssues,
  stepName,
  validateProcess,
  type CheckableEdge,
  type CheckableNode,
} from './processValidation';

const node = (id: string, type: string, data: Record<string, unknown> = {}): CheckableNode => ({
  id,
  type,
  data,
});
const edge = (id: string, source: string, target: string, condition?: string): CheckableEdge => ({
  id,
  source,
  target,
  data: condition === undefined ? {} : { condition },
});

/** start → approve → end, all connected, nothing to complain about. */
function straightThrough(): { nodes: CheckableNode[]; edges: CheckableEdge[] } {
  return {
    nodes: [
      node('s', 'startEvent', { label: 'Expense submitted' }),
      node('a', 'userTask', { label: 'Manager approves' }),
      node('e', 'endEvent', { label: 'Done' }),
    ],
    edges: [edge('f1', 's', 'a'), edge('f2', 'a', 'e')],
  };
}

describe('naming a step in a message', () => {
  it('uses the name the author gave it', () => {
    expect(stepName(node('n1', 'userTask', { label: 'Manager approves' }))).toBe('Manager approves');
  });

  /*
   * An id in an error message is a dead end for the person reading it. When
   * there is no name, say what kind of step it is.
   */
  it('falls back to what that kind of step is called, never straight to the id', () => {
    expect(stepName(node('Activity_1x2y', 'userTask'))).toBe('Ask a person');
  });

  it('uses the id only when nothing else is known', () => {
    expect(stepName(node('mystery', 'notARealType'))).toBe('mystery');
  });
});

describe('a process that is fine', () => {
  it('reports nothing', () => {
    const { nodes, edges } = straightThrough();
    expect(validateProcess(nodes, edges)).toEqual([]);
  });
});

describe('the shape of the process', () => {
  it('says plainly when there is nothing on the canvas', () => {
    const issues = validateProcess([], []);
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toBe('This process is empty.');
  });

  it('blocks a process with no start', () => {
    const issues = validateProcess([node('a', 'userTask', { label: 'Approve' }), node('e', 'endEvent')], [edge('f', 'a', 'e')]);
    expect(issues.some((i) => i.severity === 'error' && i.message.includes('no starting point'))).toBe(true);
  });

  it('blocks a process that never finishes', () => {
    const issues = validateProcess([node('s', 'startEvent'), node('a', 'userTask', { label: 'Approve' })], [edge('f', 's', 'a')]);
    expect(issues.some((i) => i.severity === 'error' && i.message.includes('never finishes'))).toBe(true);
  });

  it('names a step that can never be reached', () => {
    const { nodes, edges } = straightThrough();
    nodes.push(node('orphan', 'userTask', { label: 'Forgotten review' }));
    const issues = validateProcess(nodes, edges);
    const unreachable = issues.find((i) => i.message.includes('Forgotten review'));
    expect(unreachable?.severity).toBe('error');
    expect(unreachable?.message).toBe('"Forgotten review" can never be reached.');
    expect(unreachable?.suggestion).toBeTruthy();
  });

  /* A boundary event hangs off its activity; no arrow leads into it. */
  it('does not call a boundary event unreachable', () => {
    const { nodes, edges } = straightThrough();
    nodes.push(node('b', 'timerBoundaryEvent', { label: 'After three days' }));
    edges.push(edge('f3', 'b', 'e'));
    const issues = validateProcess(nodes, edges);
    expect(issues.some((i) => i.message.includes('can never be reached'))).toBe(false);
  });
});

/**
 * The check the designer was missing entirely.
 *
 * The server refuses to guess a branch — the implicit default flow ships off —
 * so a gateway whose paths carry no conditions raises an incident on the first
 * instance that reaches it. That has to be caught before deploy, not in
 * production.
 */
describe('a gateway that cannot decide', () => {
  function gatewayProcess(opts: { conditions: (string | undefined)[]; defaultFlow?: string }) {
    const nodes: CheckableNode[] = [
      node('s', 'startEvent'),
      node('g', 'exclusiveGateway', { label: 'Who approves?', ...(opts.defaultFlow ? { defaultFlow: opts.defaultFlow } : {}) }),
      node('m', 'userTask', { label: 'Manager approves' }),
      node('d', 'userTask', { label: 'Director approves' }),
      node('e', 'endEvent'),
    ];
    const edges: CheckableEdge[] = [
      edge('f0', 's', 'g'),
      edge('f1', 'g', 'm', opts.conditions[0]),
      edge('f2', 'g', 'd', opts.conditions[1]),
      edge('f3', 'm', 'e'),
      edge('f4', 'd', 'e'),
    ];
    return { nodes, edges };
  }

  it('blocks a deploy when no path says when it is taken', () => {
    const { nodes, edges } = gatewayProcess({ conditions: [undefined, undefined] });
    const issues = validateProcess(nodes, edges);
    const issue = issues.find((i) => i.message.includes('Who approves?'));
    expect(issue?.severity).toBe('error');
    expect(issue?.message).toContain('Manager approves');
    expect(issue?.message).toContain('Director approves');
    expect(issue?.suggestion).toContain('fallback');
  });

  it('blocks a deploy when one path has no condition and is not the fallback', () => {
    const { nodes, edges } = gatewayProcess({ conditions: ['amount > 1000', undefined] });
    const issues = validateProcess(nodes, edges);
    expect(issues.some((i) => i.severity === 'error' && i.message.includes('Who approves?'))).toBe(true);
  });

  it('accepts a blank path when it is named as the fallback', () => {
    const { nodes, edges } = gatewayProcess({ conditions: ['amount > 1000', undefined], defaultFlow: 'f2' });
    const issues = validateProcess(nodes, edges);
    expect(issues.some((i) => i.message.includes('Who approves?'))).toBe(false);
  });

  it('accepts a gateway where every path says when it is taken', () => {
    const { nodes, edges } = gatewayProcess({ conditions: ['amount > 1000', 'amount <= 1000'] });
    expect(validateProcess(nodes, edges).some((i) => i.message.includes('Who approves?'))).toBe(false);
  });

  it('points out a gateway with only one way out', () => {
    const nodes = [
      node('s', 'startEvent'),
      node('g', 'exclusiveGateway', { label: 'Pointless choice' }),
      node('e', 'endEvent'),
    ];
    const edges = [edge('f0', 's', 'g'), edge('f1', 'g', 'e', 'always')];
    const issues = validateProcess(nodes, edges);
    expect(issues.some((i) => i.message.includes('decides nothing'))).toBe(true);
  });
});

describe('what stops a deploy', () => {
  it('is errors, not warnings', () => {
    expect(hasBlockingIssues([{ message: 'x', severity: 'warning' }])).toBe(false);
    expect(hasBlockingIssues([{ message: 'x', severity: 'error' }])).toBe(true);
  });
});

describe('a step that fills in fields for its connector', () => {
  const schemas = new Map([
    [
      'lookup-id',
      [
        { key: 'connector_statement', label: 'Query', type: 'textarea', required: true },
        { key: 'connector_params', label: 'Values', type: 'mapping' },
        { key: 'result_variable', label: 'Store the answer as', type: 'string', required: true },
      ],
    ],
  ]);
  const lookupProcess = (data: Record<string, unknown>) => ({
    nodes: [
      node('s', 'startEvent', { label: 'Order received' }),
      node('l', 'serviceTask', { label: 'Look up the customer', connector_id: 'lookup-id', ...data }),
      node('e', 'endEvent', { label: 'Done' }),
    ],
    edges: [edge('f1', 's', 'l'), edge('f2', 'l', 'e')],
  });

  it('blocks a lookup with no query and nowhere to put the answer, naming the step', () => {
    const { nodes, edges } = lookupProcess({});
    const issues = validateProcess(nodes, edges, schemas);
    expect(hasBlockingIssues(issues)).toBe(true);
    expect(issues[0].message).toBe('"Look up the customer" is missing “Query” and “Store the answer as”.');
    expect(issues[0].id).toBe('l');
  });

  it('blocks a query that uses a value the step never gives', () => {
    const { nodes, edges } = lookupProcess({
      connector_statement: 'SELECT tier FROM customers WHERE id = :customer_id',
      result_variable: 'customer',
    });
    const issues = validateProcess(nodes, edges, schemas);
    expect(issues.map((issue) => issue.message)).toEqual([
      'The query in "Look up the customer" uses :customer_id without saying where the value comes from.',
    ]);
  });

  it('is content with a complete lookup', () => {
    const { nodes, edges } = lookupProcess({
      connector_statement: 'SELECT tier FROM customers WHERE id = :customer_id',
      connector_params: { customer_id: 'customerId' },
      result_variable: 'customer',
    });
    expect(validateProcess(nodes, edges, schemas)).toEqual([]);
  });

  it('leaves every other step alone, and checks nothing without the catalogue', () => {
    const { nodes, edges } = lookupProcess({});
    expect(validateProcess(nodes, edges)).toEqual([]);
  });
});
