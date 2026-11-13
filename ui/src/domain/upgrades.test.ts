import { describe, expect, it } from 'bun:test';

import type { UpgradeStatus } from '../services/domains/upgradeService';
import {
  checkedLabel,
  installGuidance,
  migrationLines,
  migrationsLabel,
  progressView,
  upgradeAvailable,
  upgradeInFlight,
} from './upgrades';

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
    can_start: false,
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

describe('progressView', () => {
  const progress = {
    from: 'v0.8.2',
    to: 'v0.9.0',
    step: 'backed up',
    started_at: '2026-11-13T09:00:00Z',
    updated_at: '2026-11-13T09:01:00Z',
    steps: [],
  };

  it('says nothing when no upgrade was asked for', () => {
    expect(progressView(status())).toBeNull();
    expect(upgradeInFlight(status())).toBe(false);
  });

  it('says who asked, and keeps asking the server while it waits', () => {
    const waiting = status({ requested: { to: 'v0.9.0', requested_by: 'root', requested_at: '2026-11-13T09:00:00Z' } });
    expect(progressView(waiting)?.title).toBe('Upgrade to v0.9.0 asked for');
    expect(progressView(waiting)?.body).toContain('root asked for it');
    expect(upgradeInFlight(waiting)).toBe(true);
  });

  it('names the step while running', () => {
    const running = status({ progress: { ...progress, outcome: 'running' } });
    expect(progressView(running)?.body).toContain('Now: backed up');
    expect(upgradeInFlight(running)).toBe(true);
  });

  it('is quiet about an upgrade that ended in the version running', () => {
    expect(progressView(status({ current: 'v0.9.0', progress: { ...progress, outcome: 'done' } }))).toBeNull();
  });

  it('says nothing changed when the upgrade was refused', () => {
    const view = progressView(status({ progress: { ...progress, outcome: 'refused', detail: 'verify v0.9.0: not verified' } }));
    expect(view?.color).toBe('orange');
    expect(view?.body).toBe('Nothing on this host was changed. verify v0.9.0: not verified');
  });

  it('names the backup when the database needs a restore', () => {
    const view = progressView(status({ progress: { ...progress, outcome: 'needs_restore', backup: '/var/lib/metis/backups/x.dump' } }));
    expect(view?.color).toBe('red');
    expect(view?.body).toContain('Restore /var/lib/metis/backups/x.dump and reinstall v0.8.2');
    expect(upgradeInFlight(status({ progress: { ...progress, outcome: 'needs_restore' } }))).toBe(false);
  });

  it('says the previous release is back after a rollback', () => {
    expect(progressView(status({ progress: { ...progress, outcome: 'rolled_back' } }))?.title).toBe(
      'v0.9.0 did not come up, so v0.8.2 is serving again',
    );
  });
});
