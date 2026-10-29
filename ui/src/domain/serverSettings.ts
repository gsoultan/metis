import type { ServerSetting } from '../services/domains/serverSettingsService';

/** The order the Settings page shows the groups in: what people ask about first. */
export const SETTING_GROUPS = [
  'email',
  'sign_in',
  'retention',
  'workers',
  'limits',
  'caches',
  'observability',
] as const;

export interface SettingGroup {
  group: string;
  settings: ServerSetting[];
}

/**
 * The settings under their groups, in the page's order. A group the server
 * sends that this build does not know goes last rather than missing: a newer
 * server's setting is still one an administrator can change.
 */
export function groupSettings(settings: ServerSetting[]): SettingGroup[] {
  const known: string[] = [...SETTING_GROUPS];
  const order = [...known, ...new Set(settings.map((s) => s.group).filter((g) => !known.includes(g)))];
  return order
    .map((group) => ({ group, settings: settings.filter((s) => s.group === group) }))
    .filter((g) => g.settings.length > 0);
}

/** Whether any saved setting waits for a restart of this server. */
export function waitsForRestart(settings: ServerSetting[]): boolean {
  return settings.some((s) => s.pending_restart);
}

/** What the page shows as a setting's value. A secret is only ever set or not. */
export function shownValue(setting: ServerSetting, notSet: string, hidden: string): string {
  if (setting.secret) {
    return setting.is_set ? hidden : notSet;
  }
  return setting.value || setting.default || notSet;
}

/**
 * Why a draft cannot be sent, in the server's terms, checked before the round
 * trip. The server checks again, with the bounds it reads the setting with;
 * this only catches what is plainly not a value of the kind.
 */
export function draftProblem(setting: ServerSetting, value: string, reason: string): 'value' | 'reason' | null {
  const trimmed = value.trim();
  if (trimmed === '' || trimmed !== value) {
    return 'value';
  }
  if ((setting.kind === 'integer' || setting.kind === 'days') && !/^\d+$/.test(trimmed)) {
    return 'value';
  }
  if (setting.kind === 'boolean' && trimmed !== 'true' && trimmed !== 'false') {
    return 'value';
  }
  if (reason.trim() === '') {
    return 'reason';
  }
  return null;
}
