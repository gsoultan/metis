import { describe, expect, test } from 'bun:test';
import { draftProblem, groupSettings, shownValue, waitsForRestart } from './serverSettings';
import type { ServerSetting } from '../services/domains/serverSettingsService';

const setting = (over: Partial<ServerSetting>): ServerSetting => ({
  name: 'X', group: 'email', label: 'X', help: '', default: '', kind: 'text', secret: false,
  applies: 'restart', source: 'default', value: '', is_set: false, pending_restart: false, ...over,
});

describe('groupSettings', () => {
  test('puts groups in the page order and an unknown one last', () => {
    const grouped = groupSettings([
      setting({ name: 'A', group: 'workers' }),
      setting({ name: 'B', group: 'from-a-newer-server' }),
      setting({ name: 'C', group: 'email' }),
    ]);
    expect(grouped.map((g) => g.group)).toEqual(['email', 'workers', 'from-a-newer-server']);
  });
});

describe('shownValue', () => {
  test('never shows a secret, only whether one is set', () => {
    expect(shownValue(setting({ secret: true, is_set: true, value: 'leak' }), 'not set', 'set')).toBe('set');
    expect(shownValue(setting({ secret: true }), 'not set', 'set')).toBe('not set');
  });

  test('shows the default when nothing is set', () => {
    expect(shownValue(setting({ default: '587' }), 'not set', 'set')).toBe('587');
  });
});

describe('draftProblem', () => {
  test('asks for a value and a reason', () => {
    expect(draftProblem(setting({}), '', 'why')).toBe('value');
    expect(draftProblem(setting({}), 'smtp.example.com', ' ')).toBe('reason');
    expect(draftProblem(setting({}), 'smtp.example.com', 'new relay')).toBeNull();
  });

  test('refuses what is plainly not a number or a switch', () => {
    expect(draftProblem(setting({ kind: 'integer' }), 'ten', 'why')).toBe('value');
    expect(draftProblem(setting({ kind: 'boolean' }), 'yes', 'why')).toBe('value');
  });
});

test('waitsForRestart', () => {
  expect(waitsForRestart([setting({}), setting({ pending_restart: true })])).toBe(true);
  expect(waitsForRestart([setting({})])).toBe(false);
});
