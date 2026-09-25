import { describe, expect, it } from 'bun:test';

import {
  diffSummary,
  diffVersions,
  landingChoices,
  proposeMapping,
  removedNodes,
  rolloutEffect,
} from './versionDiff';
import type { ApiDefinition, ApiNode } from '../services/types';

const node = (over: Partial<ApiNode> & { id: string }): ApiNode => ({
  name: over.id,
  type: 'userTask',
  x: 0,
  y: 0,
  ...over,
});

const version = (v: number, nodes: ApiNode[]): ApiDefinition => ({
  id: `def-${v}`,
  project_id: 'p',
  key: 'quotation',
  name: 'Quotation',
  version: v,
  nodes,
  flows: [],
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
      'The two versions have the same steps, configured the same way.',
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
