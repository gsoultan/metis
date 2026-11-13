import { describe, expect, it } from 'bun:test';

import type { UpgradeStatus } from '../services/domains/upgradeService';
import { checkedLabel, installGuidance, migrationLines, migrationsLabel, upgradeAvailable } from './upgrades';

function status(over: Partial<UpgradeStatus> = {}): UpgradeStatus {
  return {
    current: 'v0.8.2',
    current_is_release: true,
    enabled: true,
    channel: 'stable',
    install: 'package',
    schema_version: 55,
    pending: [],
    reachable: true,
    ...over,
  };
}

const newest = {
  tag: 'v0.9.0',
  name: 'v0.9.0',
  published_at: '2026-11-11T10:00:00Z',
  url: 'https://github.com/gsoultan/metis/releases/tag/v0.9.0',
  notes: '',
  prerelease: false,
};

const manifest = { release: 'v0.9.0', schema_version: 56, oldest_schema: 42, via_release: '0.6.0', migrations: [] };

describe('upgradeAvailable', () => {
  it('offers nothing before the status arrives, or when nothing is newer', () => {
    expect(upgradeAvailable(undefined)).toBe(false);
    expect(upgradeAvailable(status())).toBe(false);
  });

  it('offers a newer release', () => {
    expect(upgradeAvailable(status({ newest }))).toBe(true);
  });

  it('offers nothing when the check is turned off', () => {
    expect(upgradeAvailable(status({ newest, enabled: false }))).toBe(false);
  });
});

describe('migrationsLabel', () => {
  it('says nothing when the release did not say what it changes', () => {
    expect(migrationsLabel(status({ newest }))).toBeNull();
  });

  it.each([
    [[], 'No migrations'],
    [[{ version: 56, name: 'a' }], '1 migration'],
    [[{ version: 55, name: 'a' }, { version: 56, name: 'b' }], '2 migrations'],
  ])('counts what would run', (pending, label) => {
    expect(migrationsLabel(status({ newest: { ...newest, manifest }, pending }))).toBe(label);
  });

  it('treats a missing list as none', () => {
    expect(migrationsLabel(status({ newest: { ...newest, manifest }, pending: null }))).toBe('No migrations');
  });
});

describe('migrationLines', () => {
  it('names each migration in order', () => {
    expect(migrationLines([{ version: 55, name: 'person devices' }])).toEqual(['Migration 55: person devices']);
    expect(migrationLines(null)).toEqual([]);
  });
});

describe('installGuidance', () => {
  it('says the orchestrator owns a container', () => {
    expect(installGuidance('container')).toContain('owns the image');
  });

  it('says to back up first, whatever the install', () => {
    for (const kind of ['package', 'container', 'binary'] as const) {
      expect(installGuidance(kind)).toContain('backup');
    }
  });
});

describe('checkedLabel', () => {
  const now = new Date('2026-11-13T08:00:00Z');
  it.each([
    [undefined, 'Not checked yet'],
    ['2026-11-13T07:59:50Z', 'Just now'],
    ['2026-11-13T07:59:00Z', '1 minute ago'],
    ['2026-11-13T07:48:00Z', '12 minutes ago'],
    ['2026-11-13T06:50:00Z', '1 hour ago'],
    ['2026-11-13T02:00:00Z', '6 hours ago'],
  ])('%s reads %s', (checkedAt, label) => {
    expect(checkedLabel(checkedAt, now)).toBe(label);
  });
});
