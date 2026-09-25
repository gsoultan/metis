import { describe, expect, it } from 'bun:test';

import {
  actionConsequence,
  canApply,
  carriedNodes,
  hasAdvisories,
  heldTasksAffected,
  isApplicable,
  migrationRequestKey,
  movedNodes,
  planSummary,
  removedNodesSummary,
  tasksAffected,
  toNodeActions,
  toNodeMapping,
  workOn,
} from './instanceMigration';
import type { ApiMigrationPlan } from '../services/types';

const plan = (over: Partial<ApiMigrationPlan> = {}): ApiMigrationPlan => ({
  source_key: 'expense-approval',
  source_version: 1,
  target_version: 2,
  target_id: 'def-2',
  instances: 3,
  moves: [
    { from: 'approve', to: 'review', tokens: 3, tasks: 3, jobs: 0, mapped: true },
    { from: 'notify', to: 'notify', tokens: 0, tasks: 0, jobs: 1, mapped: false },
  ],
  ...over,
});

describe('instanceMigration', () => {
  it('counts every kind of work on a node', () => {
    expect(workOn({ from: 'a', to: 'b', tokens: 2, tasks: 1, jobs: 4, mapped: true })).toBe(7);
  });

  it('separates the nodes that move from the ones carried across', () => {
    expect(movedNodes(plan()).map((m) => m.from)).toEqual(['approve']);
    expect(carriedNodes(plan()).map((m) => m.from)).toEqual(['notify']);
  });

  it('treats a plan with refusals as not applicable', () => {
    expect(isApplicable(plan())).toBe(true);
    expect(isApplicable(plan({ refusals: ['nowhere to put approve'] }))).toBe(false);
    expect(isApplicable(null)).toBe(false);
  });

  it('counts the people affected, not the instances', () => {
    expect(tasksAffected(plan())).toBe(3);
  });

  it('says plainly when there is nothing to move', () => {
    expect(planSummary(plan({ instances: 0, moves: [] }))).toBe(
      'Nothing is running on version 1. There is nothing to move.',
    );
  });

  it('names the inboxes, because those are people', () => {
    expect(planSummary(plan())).toBe(
      "3 instances would move from version 1 to version 2, including 3 tasks already in somebody's inbox.",
    );
  });

  it('does not mention inboxes when no task moves', () => {
    expect(planSummary(plan({ moves: [{ from: 'wait', to: 'hold', tokens: 3, tasks: 0, jobs: 3, mapped: true }] }))).toBe(
      '3 instances would move from version 1 to version 2.',
    );
  });

  it('reads one instance as one instance', () => {
    expect(planSummary(plan({ instances: 1, moves: [] }))).toBe(
      '1 instance would move from version 1 to version 2.',
    );
  });

  it('drops rows nobody has finished filling in', () => {
    expect(
      toNodeMapping([
        { from: 'approve', to: 'review' },
        { from: 'notify', to: '' },
        { from: '', to: 'somewhere' },
        { from: '  spaced  ', to: '  padded ' },
      ]),
    ).toEqual({ approve: 'review', spaced: 'padded' });
  });

  it('drops a row that maps a node onto itself', () => {
    // Sending it would be asking the server to do nothing, in a request whose
    // whole purpose is to say what changes.
    expect(toNodeMapping([{ from: 'notify', to: 'notify' }])).toEqual({});
  });

  it('counts held tasks apart from tasks, because those are interruptions', () => {
    expect(
      heldTasksAffected(
        plan({
          moves: [
            { from: 'approve', to: 'review', tokens: 3, tasks: 3, tasks_claimed: 2, tasks_delegated: 1, mapped: true },
            // Carried across, so nobody is interrupted.
            { from: 'notify', to: 'notify', tokens: 0, tasks: 4, tasks_claimed: 4, jobs: 0, mapped: false },
          ],
        }),
      ),
    ).toBe(3);
  });

  it('treats an applicable plan with warnings as still worth reading', () => {
    // The case that motivated this: it applies cleanly and then produces
    // incidents, so "no refusals" must not read as "nothing to see".
    const warned = plan({ warnings: ['gateway "route" has a default flow'] });
    expect(isApplicable(warned)).toBe(true);
    expect(hasAdvisories(warned)).toBe(true);
    expect(hasAdvisories(plan())).toBe(false);
    expect(hasAdvisories(null)).toBe(false);
  });

  it('drops action rows nobody has finished, including the reason', () => {
    // The reason is not decoration: without it the trail cannot tell a skipped
    // approval from one somebody gave, and the server refuses it for that.
    expect(
      toNodeActions([
        { from: 'opsApprove', kind: 'skip', reason: 'role eliminated' },
        { from: 'other', kind: 'skip', reason: '   ' },
        { from: 'third', kind: '', reason: 'a reason' },
        { from: '  ', kind: 'cancel', reason: 'a reason' },
      ]),
    ).toEqual({ opsApprove: { kind: 'skip', reason: 'role eliminated' } });
  });

  it('describes a decision by what happens to the work, not to the token', () => {
    expect(actionConsequence('skip', 'opsApprove')).toContain('advance to the next step');
    expect(actionConsequence('cancel', 'opsApprove')).toContain('end here');
    expect(actionConsequence('cancel', 'opsApprove')).toContain('keeps the version they ran');
    // A hold is the one that changes nothing about the instance, only where it
    // is visible, and the wording has to carry that.
    expect(actionConsequence('hold', 'opsApprove')).toContain('left exactly as they are');
    expect(actionConsequence('hold', 'opsApprove')).toContain('incidents');
  });

  it('says what a removed step stops setting, not just that it is gone', () => {
    expect(removedNodesSummary(plan({ removed_nodes: ['opsApprove'] }))).toBe(
      'Version 2 no longer has "opsApprove". Anything that step used to set is no longer set.',
    );
    expect(removedNodesSummary(plan({ removed_nodes: ['a', 'b'] }))).toBe(
      'Version 2 no longer has "a", "b". Anything those steps used to set is no longer set.',
    );
    expect(removedNodesSummary(plan())).toBeNull();
  });
});

describe('canApply', () => {
  it('does not apply a plan worked out for something other than what is on screen', () => {
    // The mapping was edited, a hold was ticked or the live version moved on:
    // the plan in hand answers the request before that, and the one for this
    // request is still being worked out. Pressing "Move" now would apply
    // something nobody has seen a plan for.
    expect(canApply({ plan: plan(), fresh: false, error: null, applying: false })).toBe(false);
  });

  it('applies a plan that answers what is on screen', () => {
    expect(canApply({ plan: plan(), fresh: true, error: null, applying: false })).toBe(true);
  });

  it('does not apply a second time while the first apply is on its way', () => {
    // A double click, or Enter held on the button, used to send the move
    // twice: the button only disabled itself a render later. The server does
    // not refuse the second, and every instance it re-reads as still running
    // gets a second "migrated" entry on its trail.
    expect(canApply({ plan: plan(), fresh: true, error: null, applying: true })).toBe(false);
  });

  it('does not apply a plan that refuses, has nothing to move, or could not be worked out', () => {
    expect(canApply({ plan: plan({ refusals: ['nowhere to put approve'] }), fresh: true, error: null, applying: false })).toBe(false);
    expect(canApply({ plan: plan({ instances: 0, moves: [] }), fresh: true, error: null, applying: false })).toBe(false);
    expect(canApply({ plan: null, fresh: true, error: 'version 5 has no node for a→b', applying: false })).toBe(false);
  });
});

describe('migrationRequestKey', () => {
  const request = {
    source: 'def-2',
    target: 'def-5',
    mapping: { opsApprove: 'salesApprove', legal: 'review' },
    acknowledge: ['opsApprove'],
    actions: { credit: { kind: 'skip' as const, reason: 'waived' } },
  };
  const key = migrationRequestKey(request);

  it('changes with anything the server would be sent', () => {
    expect(migrationRequestKey({ ...request, target: 'def-6' })).not.toBe(key);
    expect(migrationRequestKey({ ...request, mapping: { opsApprove: 'financeApprove', legal: 'review' } })).not.toBe(key);
    expect(migrationRequestKey({ ...request, acknowledge: [] })).not.toBe(key);
    expect(migrationRequestKey({ ...request, actions: { credit: { kind: 'cancel', reason: 'waived' } } })).not.toBe(key);
  });

  it('does not change with the order it was written in', () => {
    expect(migrationRequestKey({ ...request, mapping: { legal: 'review', opsApprove: 'salesApprove' } })).toBe(key);
  });
});
