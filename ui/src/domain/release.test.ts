import { describe, expect, it } from 'bun:test';

import { canRollBack, parseKeys, releaseActions, type Release } from './release';

const base: Release = {
  id: 'r', project_id: 'p', name: 'r1', hash: 'h', origin: 'imported', state: 'staged',
  items: [], connections: [], cut_at: '', cut_by: 'dana', acted_by: 'lee', created_at: '',
};

describe('what a release row offers', () => {
  it('goes live from staged where nothing is protected', () => {
    expect(releaseActions(base, false)).toEqual({ approve: false, goLive: true });
  });

  it('is approved by a second person before it goes live in a protected runtime', () => {
    expect(releaseActions(base, true)).toEqual({ approve: true, goLive: false });
    expect(releaseActions({ ...base, approved_by: 'kim' }, true)).toEqual({ approve: false, goLive: true });
  });

  it('never goes live where it was cut', () => {
    expect(releaseActions({ ...base, origin: 'cut', state: 'cut' }, false)).toEqual({ approve: false, goLive: false });
  });

  it('offers nothing once live', () => {
    expect(releaseActions({ ...base, state: 'live', approved_by: 'kim' }, true)).toEqual({ approve: false, goLive: false });
  });
});

describe('rolling back', () => {
  it('needs a live release with one before it', () => {
    expect(canRollBack([{ ...base, state: 'live' }])).toBe(false);
    expect(canRollBack([{ ...base, state: 'live', previous: 'r0' }])).toBe(true);
  });
});

describe('typing process keys', () => {
  it('reads a list separated by commas or spaces, each once', () => {
    expect(parseKeys(' claims, onboarding  claims\n')).toEqual(['claims', 'onboarding']);
  });
});
