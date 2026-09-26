/**
 * Editing a step's settings as JSON: what the raw schema editor shows, and
 * what text typed into it applies.
 */

import { storedName } from '../mappers/settingNames';

/**
 * What the raw schema editor holds between keystrokes: the text as typed, and
 * a fingerprint of the settings that text leaves the step with.
 */
export interface RawDraft {
  text: string;
  expects: string;
}

/** What the raw schema editor shows, and why its text is not applied, if it is not. */
export interface RawEditorView {
  text: string;
  problem?: string;
}

/**
 * The draft, while it still stands over the step's settings, or null.
 *
 * The draft stands while the step still has the settings it expects: after its
 * own update, and while it does not parse and so has changed nothing. A change
 * made anywhere else, such as the name field beside it, replaces the draft.
 * Kept, the draft would write its older settings back on the next keystroke,
 * and it has to be dropped rather than set aside: when the step came back to
 * the settings it expects, by a rename and a rename back, the old text and its
 * error came back with them.
 */
export function liveDraft(current: Record<string, unknown>, draft: RawDraft | null): RawDraft | null {
  return draft !== null && draft.expects === settingsFingerprint(current) ? draft : null;
}

/** What the raw schema editor shows over a step's settings. */
export function rawEditorView(current: Record<string, unknown>, draft: RawDraft | null): RawEditorView {
  const live = liveDraft(current, draft);
  if (live === null) return { text: JSON.stringify(current, null, 2) };
  const reading = readRawSettings(live.text);
  return reading.ok ? { text: live.text } : { text: live.text, problem: reading.problem };
}

/**
 * One keystroke in the raw schema editor: the draft to keep, and the update to
 * apply when the text parses to settings the step does not already have.
 */
export function editRawSettings(
  current: Record<string, unknown>,
  text: string,
): { draft: RawDraft; patch?: Record<string, unknown> } {
  const reading = readRawSettings(text);
  if (!reading.ok) return { draft: { text, expects: settingsFingerprint(current) } };

  const expects = settingsFingerprint(reading.settings);
  if (expects === settingsFingerprint(current)) return { draft: { text, expects } };
  return { draft: { text, expects }, patch: replacing(current, reading.settings) };
}

type RawReading = { ok: true; settings: Record<string, unknown> } | { ok: false; problem: string };

function readRawSettings(text: string): RawReading {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    return { ok: false, problem: `This is not valid JSON yet, so it has not been applied. ${detail}` };
  }
  // Merged into the step, a list or a string would scatter its items across
  // the settings as keys.
  if (!isPlainObject(parsed)) {
    return { ok: false, problem: 'A step\'s settings are one object, in braces, so this has not been applied.' };
  }
  // Saved, both names become one, and one of the two values is lost, while
  // the editor goes on showing both.
  const twins = namesOfOneSetting(parsed);
  if (twins !== undefined) {
    return {
      ok: false,
      problem: `"${twins[0]}" and "${twins[1]}" are the same setting, so this has not been applied. Keep one of them.`,
    };
  }
  return { ok: true, settings: parsed };
}

/** Two keys that are saved as one setting, if the settings hold any. */
function namesOfOneSetting(settings: Record<string, unknown>): [string, string] | undefined {
  const seen = new Map<string, string>();
  for (const key of Object.keys(settings)) {
    const earlier = seen.get(storedName(key));
    if (earlier !== undefined) return [earlier, key];
    seen.set(storedName(key), key);
  }
  return undefined;
}

/**
 * The update that leaves a step with exactly the edited settings.
 *
 * The designer merges an update into what is there, so a key deleted in the
 * editor would survive the merge. It is sent as undefined, which saving leaves
 * out. Only keys the editor could show are cleared: a value JSON cannot hold
 * was never in the text.
 */
function replacing(current: Record<string, unknown>, edited: Record<string, unknown>): Record<string, unknown> {
  const deleted = Object.keys(current).filter((key) => isJsonValue(current[key]) && !Object.hasOwn(edited, key));
  return { ...Object.fromEntries(deleted.map((key) => [key, undefined])), ...edited };
}

/** The settings as JSON with every object's keys sorted, so key order does not count. */
function settingsFingerprint(settings: unknown): string {
  return JSON.stringify(settings, (_key, value: unknown) =>
    isPlainObject(value)
      ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => (a < b ? -1 : 1)))
      : value,
  );
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isJsonValue(value: unknown): boolean {
  return value !== undefined && typeof value !== 'function' && typeof value !== 'symbol';
}
