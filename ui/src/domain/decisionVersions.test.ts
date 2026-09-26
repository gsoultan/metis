import { describe, expect, it } from 'bun:test';

import { decisionRowVersions } from './decisionVersions';

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
