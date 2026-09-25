import { describe, expect, it } from 'bun:test';

import {
  canDelete,
  canSchedule,
  drainingVersions,
  isFutureCutover,
  isRollback,
  liveVersion,
  nextDeployStep,
  nextVersionNumber,
  pendingCutovers,
  promotionFacts,
  rolloutOutcome,
  rowVersions,
  shouldAskRollout,
  totalRunning,
  versionState,
  type VersionStatus,
} from './versionRollout';

const v = (version: number, live: boolean, running: number): VersionStatus => ({
  version,
  live,
  running_instances: running,
  total_instances: running,
});

describe('versionState', () => {
  const all = [v(3, false, 0), v(2, true, 4), v(1, false, 0)];

  it('calls the version taking new instances live', () => {
    expect(versionState(v(2, true, 4), all)).toBe('live');
  });

  it('calls a replaced version with work left draining', () => {
    expect(versionState(v(1, false, 4), all)).toBe('draining');
  });

  it('calls a version ahead of the live one, carrying nothing, staged', () => {
    // Deployed but never promoted. Labelling this "retired" — which judging the
    // row alone does — is the most discouraging possible word for something
    // waiting to go live.
    expect(versionState(v(3, false, 0), all)).toBe('staged');
  });

  it('calls a version behind the live one, carrying nothing, retired', () => {
    expect(versionState(v(1, false, 0), all)).toBe('retired');
  });

  it('keeps the live version live even when it is also running work', () => {
    // A live version normally has instances too; "draining" is specifically the
    // state of no longer receiving them, so having work is not what defines it.
    expect(versionState(v(2, true, 12), all)).toBe('live');
  });
});

describe('shouldAskRollout', () => {
  it('does not ask on the first deploy of a process', () => {
    // There is no incumbent to keep live, so the question has one answer.
    expect(shouldAskRollout([])).toBe(false);
  });

  it('asks once a version exists that could stay live', () => {
    expect(shouldAskRollout([v(1, true, 0)])).toBe(true);
  });
});

describe('nextVersionNumber', () => {
  it('is one past the highest deployed, not one past the live one', () => {
    // A staged v3 exists while v2 is live; the next deploy still claims v4.
    expect(nextVersionNumber([v(3, false, 0), v(2, true, 5)])).toBe(4);
  });

  it('starts at one when nothing is deployed', () => {
    expect(nextVersionNumber([])).toBe(1);
  });
});

describe('rolloutOutcome', () => {
  it('describes what staying on the incumbent would mean', () => {
    const versions = [v(2, true, 7), v(1, false, 3)];
    expect(rolloutOutcome(versions)).toEqual({ version: 3, incumbent: 2, draining: 7 });
  });

  it('counts only the incumbent, not every version still draining', () => {
    // v1 is draining too, but this deploy changes nothing about it, so
    // attributing its instances to the decision would overstate the stakes.
    const versions = [v(2, true, 7), v(1, false, 3)];
    expect(rolloutOutcome(versions).draining).toBe(7);
  });

  it('has no incumbent on a first deploy', () => {
    expect(rolloutOutcome([])).toEqual({ version: 1, incumbent: null, draining: 0 });
  });
});

describe('liveVersion', () => {
  it('finds the one version taking new instances', () => {
    expect(liveVersion([v(3, false, 0), v(2, true, 1)])?.version).toBe(2);
  });

  it('is null when nothing is deployed', () => {
    expect(liveVersion([])).toBeNull();
  });
});

describe('drainingVersions', () => {
  it('lists replaced versions that still hold work', () => {
    const versions = [v(3, true, 2), v(2, false, 5), v(1, false, 0)];
    expect(drainingVersions(versions).map((d) => d.version)).toEqual([2]);
  });
});

describe('totalRunning', () => {
  it('sums work across every version, live and draining', () => {
    expect(totalRunning([v(3, true, 2), v(2, false, 5), v(1, false, 0)])).toBe(7);
  });
});

describe('isRollback', () => {
  it('recognises promoting an older version', () => {
    expect(isRollback([v(3, true, 0), v(2, false, 4)], 2)).toBe(true);
  });

  it('does not call promoting a staged newer version a rollback', () => {
    expect(isRollback([v(3, false, 0), v(2, true, 4)], 3)).toBe(false);
  });

  it('is not a rollback when nothing is live yet', () => {
    expect(isRollback([], 1)).toBe(false);
  });
});

describe('rowVersions', () => {
  const versions = [
    { key: 'expense', version: 3 },
    { key: 'expense', version: 2 },
    { key: 'expense', version: 1 },
  ];

  it('reports the live version, not the newest, when one is staged', () => {
    const row = rowVersions(versions, { expense: 2 });
    expect(row?.live.version).toBe(2);
    expect(row?.staged?.version).toBe(3);
  });

  it('falls back to the highest version when nothing has been promoted', () => {
    // No release row means the engine resolves to the highest, so the list has
    // to say the same thing or it would name a version nothing starts on.
    const row = rowVersions(versions, {});
    expect(row?.live.version).toBe(3);
    expect(row?.staged).toBeNull();
  });

  it('does not call an older version staged after a rollback', () => {
    // Live is v1 and v2/v3 exist. v3 is the one waiting to be promoted; v2 is
    // just history, and listing it as staged would offer a meaningless action.
    const row = rowVersions(versions, { expense: 1 });
    expect(row?.live.version).toBe(1);
    expect(row?.staged?.version).toBe(3);
  });

  it('has nothing to say about a key with no versions', () => {
    expect(rowVersions([], {})).toBeNull();
  });
});

describe('pendingCutovers', () => {
  const rows = [
    { ...v(3, false, 0), scheduled_for: '2026-10-01T09:00:00Z', scheduled_release_id: 'r3' },
    { ...v(2, true, 4) },
    { ...v(1, false, 0), scheduled_for: '2026-09-20T09:00:00Z', scheduled_release_id: 'r1' },
  ];

  it('lists only versions with a cutover still to come, soonest first', () => {
    expect(pendingCutovers(rows).map((c) => c.version)).toEqual([1, 3]);
  });

  it('carries the release id, because that is what cancelling names', () => {
    expect(pendingCutovers(rows)[0].releaseId).toBe('r1');
  });

  it('ignores a row whose timestamp will not parse', () => {
    // A malformed date must not become an Invalid Date rendered at the person.
    const broken = [{ ...v(2, false, 0), scheduled_for: 'not a date', scheduled_release_id: 'r2' }];
    expect(pendingCutovers(broken)).toEqual([]);
  });

  it('is empty when nothing is arranged', () => {
    expect(pendingCutovers([v(1, true, 0)])).toEqual([]);
  });
});

describe('isFutureCutover', () => {
  const now = new Date('2026-09-07T12:00:00Z');

  it('accepts a time still to come', () => {
    expect(isFutureCutover(new Date('2026-09-08T12:00:00Z'), now)).toBe(true);
  });

  it('refuses a time already gone', () => {
    expect(isFutureCutover(new Date('2026-09-06T12:00:00Z'), now)).toBe(false);
  });

  it('refuses this exact instant, which is a promotion rather than a cutover', () => {
    expect(isFutureCutover(new Date(now), now)).toBe(false);
  });

  it('refuses nothing chosen, and refuses an unparseable date', () => {
    expect(isFutureCutover(null, now)).toBe(false);
    expect(isFutureCutover(new Date('nonsense'), now)).toBe(false);
  });
});

describe('canSchedule', () => {
  it('does not offer to schedule the version that is already live', () => {
    expect(canSchedule(v(2, true, 4))).toBe(false);
  });

  it('offers it for any other version', () => {
    expect(canSchedule(v(3, false, 0))).toBe(true);
  });
});

describe('canDelete', () => {
  it('offers to remove a version nothing has ever run', () => {
    expect(canDelete({ ...v(3, false, 0), total_instances: 0 })).toBe(true);
  });

  it('refuses one that is still running work', () => {
    expect(canDelete({ ...v(2, false, 4), total_instances: 4 })).toBe(false);
  });

  it('refuses one whose instances have finished — the record has to survive', () => {
    expect(canDelete({ ...v(1, false, 0), total_instances: 12 })).toBe(false);
  });

  it('refuses the live version even with nothing running yet', () => {
    expect(canDelete({ ...v(3, true, 0), total_instances: 0 })).toBe(false);
  });
});

describe('nextDeployStep', () => {
  const error = { severity: 'error' as const, message: 'No end event' };
  const warning = { severity: 'warning' as const, message: 'A step has no name' };

  it('holds a deploy that would not run', () => {
    expect(nextDeployStep([error, warning], [v(1, true, 0)])).toBe('held');
  });

  it('shows the warnings before deploying', () => {
    expect(nextDeployStep([warning], [v(1, true, 0)])).toBe('review');
  });

  it('asks whether to go live when a live version would be replaced', () => {
    expect(nextDeployStep([], [v(1, true, 3)])).toBe('ask');
  });

  it('deploys live when there is nothing to replace', () => {
    expect(nextDeployStep([], [])).toBe('live');
  });

  it('carries accepted warnings on to the question a clean deploy gets', () => {
    // "Deploy Anyway" deployed on the spot, and staged: the question was
    // skipped and the answer came out as the opposite of the button's name.
    const accepted: typeof warning[] = [];
    expect(nextDeployStep(accepted, [v(1, true, 3)])).toBe('ask');
    expect(nextDeployStep(accepted, [])).toBe('live');
  });
});

describe('promotionFacts', () => {
  // v5 is live and has four quotations in flight; v2 is still draining one.
  // Somebody rolls back to v3.
  const now = new Date('2026-09-25T09:00:00Z');
  const formatTime = (at: Date) => at.toISOString().slice(0, 16).replace('T', ' ');
  const versions = [
    { version: 5, live: true, running_instances: 4, total_instances: 9 },
    { version: 4, live: false, running_instances: 0, total_instances: 0 },
    { version: 3, live: false, running_instances: 0, total_instances: 6 },
    { version: 2, live: false, running_instances: 1, total_instances: 30 },
  ];
  const facts = (list = versions, target = 3) => promotionFacts(list, target, { now, formatTime });

  it('says it changes where new instances start, however they are started', () => {
    // Checked against the engine: a message or signal start, and a call from
    // another process that names no version, all resolve the live version.
    expect(facts()[0]).toBe(
      'From now on, new instances start on v3, including ones started by a message, a signal, ' +
        'or another process that calls this one without naming a version.',
    );
  });

  it('says what happens to everything already running, on every version', () => {
    // The server writes one row on the release timeline and touches no
    // instance. Instances pin the version they started on.
    expect(facts()).toContain(
      'Nothing already running is moved, stopped or restarted: the 4 instances on v5 finish on v5, ' +
        'and the 1 on v2 finishes on v2.',
    );
  });

  it('says so even when the live version has nothing running', () => {
    const quiet = versions.map((v) => (v.version === 5 ? { ...v, running_instances: 0 } : v));
    expect(facts(quiet)).toContain(
      'Nothing already running is moved, stopped or restarted: the 1 instance on v2 finishes on v2.',
    );
  });

  it('says how to move the work on the version being rolled back from', () => {
    expect(facts()).toContain(
      'To move the 4 on v5 onto v3 as well, use "Move work" on v5 once v3 is live.',
    );
  });

  it('does not point at moving work when going forward', () => {
    // Letting the old version drain is the normal path; the escape hatch is
    // offered to somebody undoing a mistake, not to every promotion.
    const forward = [...versions, { version: 6, live: false, running_instances: 0, total_instances: 0 }];
    expect(facts(forward, 6).some((fact) => fact.includes('Move work'))).toBe(false);
  });

  it('says the version it replaces is kept', () => {
    expect(facts()).toContain('v5 is not deleted, and can be made live again from this list.');
  });

  it('warns that a cutover already scheduled still happens', () => {
    // Promoting writes a row for now; the scheduled row is later, so the
    // timeline reaches it and the rollback is undone at that moment.
    const scheduled = [
      ...versions,
      { version: 6, live: false, running_instances: 0, total_instances: 0,
        scheduled_for: '2026-10-01T22:00:00Z', scheduled_release_id: 'r-6' },
    ];
    expect(facts(scheduled)).toContain(
      'v6 is still scheduled to take over at 2026-10-01 22:00. From then, new instances start on v6, ' +
        'not v3. Cancel it under Scheduled changes to keep v3 live.',
    );
  });

  it('says nothing is affected when nothing is running', () => {
    const idle = versions.map((v) => ({ ...v, running_instances: 0 }));
    expect(facts(idle)).toContain('Nothing is running on any version, so no instance is affected.');
  });
});
