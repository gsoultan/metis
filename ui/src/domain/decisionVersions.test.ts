import { describe, expect, it } from 'bun:test';

import {
  canDeleteDecisionVersion,
  decisionPromotionFacts,
  decisionRowVersions,
  decisionVersionState,
  isDecisionRollback,
  nextDecisionVersion,
  saveNotice,
} from './decisionVersions';

describe('decisionRowVersions', () => {
  it('names the live version, and a newer one as staged', () => {
    expect(decisionRowVersions({ live_version: 2, newest_version: 3 })).toEqual({ live: 2, staged: 3 });
  });

  it('names nothing staged when the newest version is the live one', () => {
    expect(decisionRowVersions({ live_version: 2, newest_version: 2 })).toEqual({ live: 2, staged: null });
  });

  it('does not call an older version staged after a rollback', () => {
    // v1 made live again after v3: v3 is newer and waiting, and v2 is history.
    expect(decisionRowVersions({ live_version: 1, newest_version: 3 })).toEqual({ live: 1, staged: 3 });
  });

  it('says nothing is live, and names the version that could be', () => {
    expect(decisionRowVersions({ live_version: 0, newest_version: 1 })).toEqual({ live: null, staged: 1 });
    expect(decisionRowVersions({ newest_version: 4 })).toEqual({ live: null, staged: 4 });
  });
});

const entry = (version: number, live = false) => ({ version, live });

describe('decisionVersionState', () => {
  const versions = [entry(4), entry(3, true), entry(2)];

  it('calls the version in force live', () => {
    expect(decisionVersionState(versions[1], versions)).toBe('live');
  });

  it('calls a version saved after the live one staged', () => {
    expect(decisionVersionState(versions[0], versions)).toBe('staged');
  });

  it('calls a version before the live one earlier', () => {
    expect(decisionVersionState(versions[2], versions)).toBe('earlier');
  });

  it('calls every version staged when none is live', () => {
    expect(decisionVersionState(entry(1), [entry(2), entry(1)])).toBe('staged');
  });
});

describe('nextDecisionVersion', () => {
  it('is one past the highest stored, not one past the live one', () => {
    expect(nextDecisionVersion([entry(4), entry(3, true)])).toBe(5);
    expect(nextDecisionVersion([])).toBe(1);
  });
});

describe('isDecisionRollback', () => {
  it('is a rollback only when the target is older than the live version', () => {
    const versions = [entry(3), entry(2, true), entry(1)];
    expect(isDecisionRollback(versions, 1)).toBe(true);
    expect(isDecisionRollback(versions, 3)).toBe(false);
    expect(isDecisionRollback([entry(2), entry(1)], 1)).toBe(false);
  });
});

describe('canDeleteDecisionVersion', () => {
  it('offers any version that is not live', () => {
    const versions = [entry(2, true), entry(1)];
    expect(canDeleteDecisionVersion(versions[1], versions)).toBe(true);
  });

  it('does not offer the live version while others remain', () => {
    const versions = [entry(2, true), entry(1)];
    expect(canDeleteDecisionVersion(versions[0], versions)).toBe(false);
  });

  it('offers the only version, which deletes the decision', () => {
    expect(canDeleteDecisionVersion(entry(1, true), [entry(1, true)])).toBe(true);
  });
});

describe('decisionPromotionFacts', () => {
  it('says running instances use it from their next step, and nothing already decided changes', () => {
    const facts = decisionPromotionFacts([entry(3, true), entry(2)], 2).join(' ');
    expect(facts).toContain('steps that name no version use v2, in instances already running as well');
    expect(facts).toContain('is not revisited');
    expect(facts).toContain('Steps pinned to a version keep using the version they name.');
    expect(facts).toContain('v3 is kept, and can be made live again from this list.');
  });

  it('says nothing is kept when nothing was live', () => {
    expect(decisionPromotionFacts([entry(1)], 1).join(' ')).not.toContain('is kept');
  });
});

describe('saveNotice', () => {
  it('says a live save is in force and the previous version is kept', () => {
    const notice = saveNotice({ version: 5, newVersion: true, live: true }, 'Discount', 4);
    expect(notice.title).toBe('v5 is live');
    expect(notice.message).toContain('v4 is kept in its history.');
  });

  it('says a staged save is not in use and which version stays live', () => {
    const notice = saveNotice({ version: 5, newVersion: true, live: false }, 'Discount', 4);
    expect(notice.title).toBe('Saved as v5, staged');
    expect(notice.message).toContain('v4 stays live');
  });

  it('says when nothing changed', () => {
    expect(saveNotice({ version: 4, newVersion: false, live: true }, 'Discount', 4).title).toBe('Nothing to save');
  });
});
