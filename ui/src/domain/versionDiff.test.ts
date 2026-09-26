import { describe, expect, it } from 'bun:test';

import {
  compareLoaded,
  diffSummary,
  diffVersions,
  landingChoices,
  proposeMapping,
  removedNodes,
  rolloutEffect,
} from './versionDiff';
import type { ApiDefinition, ApiFlow, ApiNode } from '../services/types';

const node = (over: Partial<ApiNode> & { id: string }): ApiNode => ({
  name: over.id,
  type: 'userTask',
  x: 0,
  y: 0,
  ...over,
});

const flow = (id: string, source: string, target: string, condition?: string): ApiFlow => ({
  id,
  source_ref: source,
  target_ref: target,
  condition,
});

const version = (v: number, nodes: ApiNode[], flows: ApiFlow[] = []): ApiDefinition => ({
  id: `def-${v}`,
  project_id: 'p',
  key: 'quotation',
  name: 'Quotation',
  version: v,
  nodes,
  flows,
});

// The case this was built for: management deletes the operations manager's
// approval, and somebody has to say where the work waiting there should go.
const v1 = version(1, [
  node({ id: 'start', type: 'startEvent' }),
  node({ id: 'supervisorReview', assignee: 'sam' }),
  node({ id: 'opsApprove', name: 'Operations approve', assignee: 'ollie' }),
  node({ id: 'salesApprove', assignee: 'sasha' }),
]);
const v2 = version(2, [
  node({ id: 'start', type: 'startEvent' }),
  node({ id: 'supervisorReview', assignee: 'sam' }),
  node({ id: 'salesApprove', assignee: 'sasha' }),
]);

describe('versionDiff', () => {
  it('finds the step the new version dropped', () => {
    const removed = removedNodes(diffVersions(v1, v2));
    expect(removed.map((change) => change.id)).toEqual(['opsApprove']);
    expect(removed[0].before?.name).toBe('Operations approve');
    expect(removed[0].after).toBeUndefined();
  });

  it('puts what needs a decision first', () => {
    // Removed before changed before added before unchanged: the first of those
    // is holding work, the last is context.
    const renamedApprover = version(2, [
      node({ id: 'start', type: 'startEvent' }),
      node({ id: 'supervisorReview', assignee: 'sam' }),
      node({ id: 'salesApprove', assignee: 'sasha-2' }),
      node({ id: 'notify', type: 'serviceTask' }),
    ]);
    const kinds = diffVersions(v1, renamedApprover).changes.map((change) => change.kind);
    expect(kinds).toEqual(['removed', 'changed', 'added', 'unchanged', 'unchanged']);
  });

  it('calls a node changed only when something a migration cares about changed', () => {
    // Moved on the canvas. Nothing about what it does or who does it.
    const moved = version(2, [
      node({ id: 'start', type: 'startEvent' }),
      node({ id: 'supervisorReview', assignee: 'sam', x: 900, y: 400 }),
      node({ id: 'opsApprove', name: 'Operations approve', assignee: 'ollie' }),
      node({ id: 'salesApprove', assignee: 'sasha' }),
    ]);
    const changed = diffVersions(v1, moved).changes.filter((c) => c.kind === 'changed');
    expect(changed).toEqual([]);
  });

  it('says what changed about a step, in the terms somebody asks about it', () => {
    const reassigned = version(2, [
      node({ id: 'start', type: 'startEvent' }),
      node({ id: 'supervisorReview', assignee: 'sam' }),
      node({ id: 'opsApprove', name: 'Operations approve', assignee: 'ollie' }),
      node({ id: 'salesApprove', assignee: 'dani', candidate_groups: [{ name: 'sales' }] }),
    ]);
    const change = diffVersions(v1, reassigned).changes.find((c) => c.id === 'salesApprove');
    expect(change?.kind).toBe('changed');
    expect(change?.differences).toEqual(['assignee: sasha → dani', 'candidate groups: (none) → sales']);
  });

  it('offers every step of the new version as somewhere work could land', () => {
    expect(landingChoices(diffVersions(v1, v2)).map((n) => n.id)).toEqual([
      'salesApprove',
      'start',
      'supervisorReview',
    ]);
  });

  it('summarises whether this is a rename or a rewrite', () => {
    expect(diffSummary(diffVersions(v1, v2))).toBe('1 step removed.');
    expect(diffSummary(diffVersions(v1, v1))).toBe(
      'The two versions have the same steps and paths, configured the same way.',
    );
  });

  it('proposes the mapping only when there is one thing it could be', () => {
    // One out, one in: a rename, and the only reading of it.
    const renamed = version(2, [
      node({ id: 'start', type: 'startEvent' }),
      node({ id: 'supervisorReview', assignee: 'sam' }),
      node({ id: 'operationsApprove', name: 'Operations approve', assignee: 'ollie' }),
      node({ id: 'salesApprove', assignee: 'sasha' }),
    ]);
    expect(proposeMapping(diffVersions(v1, renamed))).toEqual({ opsApprove: 'operationsApprove' });
  });

  it('proposes nothing when it would have to choose', () => {
    // Two candidates. A wrong guess pre-filled is the one nobody re-reads, so
    // there is no guess.
    const twoWays = version(2, [
      node({ id: 'start', type: 'startEvent' }),
      node({ id: 'supervisorReview', assignee: 'sam' }),
      node({ id: 'financeApprove' }),
      node({ id: 'operationsApprove' }),
      node({ id: 'salesApprove', assignee: 'sasha' }),
    ]);
    expect(proposeMapping(diffVersions(v1, twoWays))).toEqual({});
    // And nothing removed means nothing to propose either.
    expect(proposeMapping(diffVersions(v1, v1))).toEqual({});
  });

  it('reads a missing version as nothing rather than falling over', () => {
    expect(diffVersions(null, v2).changes.every((c) => c.kind === 'added')).toBe(true);
    expect(diffVersions(v1, null).changes.every((c) => c.kind === 'removed')).toBe(true);
    expect(diffVersions(null, null).changes).toEqual([]);
  });
});

describe('rolloutEffect', () => {
  const live = version(5, [
    node({ id: 'start', type: 'startEvent' }),
    node({ id: 'salesApprove', assignee: 'sasha' }),
  ]);
  const older = version(3, [
    node({ id: 'start', type: 'startEvent' }),
    node({ id: 'opsApprove', name: 'Operations approve' }),
    node({ id: 'salesApprove', assignee: 'dani' }),
  ]);

  it('says a step the older version still has comes back, not that it is new', () => {
    // Rolling back from 5 to 3. opsApprove exists in 3 and not in 5, so the
    // diff calls it "added" — but to somebody rolling back it returns.
    // Reading that forward is how you roll back believing you rolled forward.
    // In the diff's own order: what changed before what appeared.
    expect(rolloutEffect(diffVersions(live, older), true)).toEqual([
      '"salesApprove" changes: assignee: sasha → dani.',
      '"Operations approve" is a step again.',
    ]);
  });

  it('says the same step is new when moving forward', () => {
    expect(rolloutEffect(diffVersions(live, older), false)).toContain(
      '"Operations approve" is a new step.',
    );
  });

  it('names a step that goes away', () => {
    expect(rolloutEffect(diffVersions(older, live), false)).toContain(
      '"Operations approve" is no longer a step.',
    );
  });

  it('says nothing about steps that did not change', () => {
    expect(rolloutEffect(diffVersions(live, live), false)).toEqual([]);
  });
});

// Everything below is a way for two versions to behave differently that the
// comparison used to call "the same steps, configured the same way".
describe('versionDiff beyond the steps themselves', () => {
  const steps = [
    node({ id: 'start', name: 'Quote requested', type: 'startEvent' }),
    node({ id: 'check', name: 'Credit check', type: 'exclusiveGateway', default_flow: 'toReview' }),
    node({ id: 'review', name: 'Manual review' }),
    node({ id: 'approve', name: 'Approve quote' }),
    node({ id: 'reject', name: 'Reject quote' }),
  ];
  const paths = [
    flow('toCheck', 'start', 'check'),
    flow('toApprove', 'check', 'approve', 'amount < 500'),
    flow('toReview', 'check', 'review'),
    flow('toReject', 'review', 'reject'),
  ];
  const base = version(1, steps, paths);

  it('finds a path that was added, removed or pointed somewhere else', () => {
    const rerouted = version(2, steps, [
      flow('toCheck', 'start', 'check'),
      flow('toApprove', 'check', 'approve', 'amount < 500'),
      flow('toReview', 'check', 'reject'),
      flow('reviewed', 'review', 'approve'),
    ]);
    const changes = diffVersions(base, rerouted).flows;
    expect(changes.map((change) => [change.kind, change.id])).toEqual([
      ['removed', 'toReject'],
      ['changed', 'toReview'],
      ['added', 'reviewed'],
    ]);
    // Named by the steps it joins, not by its id.
    expect(changes[0].route).toBe('from "Manual review" to "Reject quote"');
    expect(changes[1].differences).toEqual(['now leads to "Reject quote"']);
    expect(changes[2].route).toBe('from "Manual review" to "Approve quote"');
  });

  it('finds a path whose condition changed', () => {
    const stricter = version(2, steps, [
      flow('toCheck', 'start', 'check'),
      flow('toApprove', 'check', 'approve', 'amount < 100'),
      flow('toReview', 'check', 'review'),
      flow('toReject', 'review', 'reject'),
    ]);
    const [change] = diffVersions(base, stricter).flows;
    expect(change.route).toBe('from "Credit check" to "Approve quote"');
    expect(change.differences).toEqual(['condition: amount < 500 → amount < 100']);
  });

  it('does not report a path that was only drawn again', () => {
    // Same two steps, same condition, a new id: the designer redrew the
    // arrow. Nothing a new instance could notice.
    const redrawn = version(2, steps, [
      flow('toCheck', 'start', 'check'),
      flow('edge-7f3a', 'check', 'approve', 'amount < 500'),
      flow('toReview', 'check', 'review'),
      flow('toReject', 'review', 'reject'),
    ]);
    expect(diffVersions(base, redrawn).flows).toEqual([]);
  });

  it('says a script changed without printing it', () => {
    const scripted = (script: string) =>
      version(1, [node({ id: 'price', name: 'Work out the price', type: 'scriptTask', script, condition: script })]);
    const change = diffVersions(scripted('total = net * 1.2'), scripted('total = net * 1.25')).changes[0];
    expect(change.kind).toBe('changed');
    // Once, although the designer stores the script twice.
    expect(change.differences).toEqual(['script changed']);
  });

  it('says what changed in a step’s settings, in the words of the property panel', () => {
    const charge = (properties: Record<string, unknown>) =>
      version(1, [node({ id: 'charge', name: 'Charge the card', type: 'serviceTask', properties })]);
    const change = diffVersions(
      charge({ implementation: 'push', http_url: 'https://pay.example/v1/charge', auth_token: 'tok_live_old' }),
      charge({ implementation: 'push', http_url: 'https://pay.example/v2/charge', auth_token: 'tok_live_new' }),
    ).changes[0];
    expect(change.differences).toEqual([
      // A credential changed, and that is all a comparison may say about it.
      'credentials changed',
      'web address: https://pay.example/v1/charge → https://pay.example/v2/charge',
    ]);
  });

  it('reads a timer’s wait as a length of time', () => {
    const waiting = (duration: string) =>
      version(1, [
        node({
          id: 'cooling',
          name: 'Cooling-off period',
          type: 'intermediateCatchEvent',
          condition: duration,
          properties: { event_type: 'timer', timer_type: 'duration', timer_duration: duration },
        }),
      ]);
    expect(diffVersions(waiting('PT5M'), waiting('P1DT12H')).changes[0].differences).toEqual([
      'wait: 5 minutes → 1 day, 12 hours',
    ]);
  });

  it('names the form fields that came and went', () => {
    const form = (fields: Array<{ id: string; label: string; type: string }>) =>
      version(1, [node({ id: 'approve', name: 'Approve quote', properties: { form_definition: fields } })]);
    const change = diffVersions(
      form([
        { id: 'approved', label: 'Approved', type: 'boolean' },
        { id: 'marginReason', label: 'Margin override reason', type: 'textarea' },
      ]),
      form([
        { id: 'approved', label: 'Approved', type: 'boolean' },
        { id: 'riskTier', label: 'Risk tier', type: 'select' },
      ]),
    ).changes[0];
    expect(change.differences).toEqual([
      'form field "Risk tier" added',
      'form field "Margin override reason" removed',
    ]);
  });

  it('follows the fallback path to where it leads', () => {
    const fallback = version(2, [...steps.filter((n) => n.id !== 'check'),
      node({ id: 'check', name: 'Credit check', type: 'exclusiveGateway', default_flow: 'toReject2' }),
    ], [...paths, flow('toReject2', 'check', 'reject')]);
    const change = diffVersions(base, fallback).changes.find((c) => c.id === 'check');
    expect(change?.differences).toEqual(['fall back to: "Manual review" → "Reject quote"']);
  });

  it('leaves out what only the designer reads', () => {
    // Sample data for a test run, and which tab the start event's editor was
    // on, change nothing about what the process does.
    const start = (properties: Record<string, unknown>) =>
      version(1, [node({ id: 'start', type: 'startEvent', properties })]);
    expect(
      diffVersions(start({ sampleData: '{"amount": 1}', startedBy: 'manual' }), start({ sampleData: '{"amount": 9}' }))
        .changes[0].kind,
    ).toBe('unchanged');
  });

  it('never calls two versions that behave differently the same', () => {
    const rerouted = version(2, steps, paths.map((p) => (p.id === 'toApprove' ? { ...p, condition: 'amount < 100' } : p)));
    expect(diffSummary(diffVersions(base, rerouted))).toBe('1 path changed.');
    expect(diffSummary(diffVersions(base, base))).toBe(
      'The two versions have the same steps and paths, configured the same way.',
    );
  });

  it('tells a rollback what happens to the paths, in the direction travelled', () => {
    const live = version(5, steps, paths.filter((p) => p.id !== 'toReject'));
    expect(rolloutEffect(diffVersions(live, base), true)).toEqual([
      'The path from "Manual review" to "Reject quote" is back.',
    ]);
    expect(rolloutEffect(diffVersions(live, base), false)).toEqual([
      'There is a new path from "Manual review" to "Reject quote".',
    ]);
    expect(rolloutEffect(diffVersions(base, live), false)).toEqual([
      'There is no longer a path from "Manual review" to "Reject quote".',
    ]);
  });
});

// A version that failed to load was compared against nothing, so every step of
// the other one read as removed or added: a total rewrite, shown as fact.
describe('compareLoaded', () => {
  const loaded = (label: string, definition: ApiDefinition | null, failed = false) =>
    ({ label, loading: false, failed, definition });
  const v2 = {
    id: 'd-2', project_id: 'p', key: 'refund', name: 'Refund', version: 2,
    nodes: [{ id: 'start', name: '', type: 'startEvent', x: 0, y: 0 }, { id: 'end', name: '', type: 'endEvent', x: 0, y: 0 }],
    flows: [{ id: 'f1', source_ref: 'start', target_ref: 'end' }],
  } as ApiDefinition;

  it('says which version could not be loaded instead of listing everything as changed', () => {
    expect(compareLoaded(loaded('v1', null, true), loaded('v2', v2))).toEqual({ kind: 'failed', missing: ['v1'] });
  });

  it('waits while either version is loading', () => {
    expect(compareLoaded({ ...loaded('v1', null), loading: true }, loaded('v2', v2))).toEqual({ kind: 'loading' });
  });

  it('compares two versions that loaded', () => {
    const comparison = compareLoaded(loaded('v1', v2), loaded('v2', v2));
    expect(comparison.kind).toBe('ready');
  });
});
