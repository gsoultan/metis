/**
 * Progressive disclosure: what basic mode shows, and what expert mode adds.
 *
 * Basic mode keeps settings that a business user rarely needs out of the way.
 * It must never hide one that is in effect. A step that runs a script does so
 * whichever mode the person looking at it is in, and a setting nobody can see
 * is one nobody can explain, check or undo. So basic mode hides a setting only
 * while it is empty. Once it is set, basic mode shows it read-only, and expert
 * mode is where it is changed.
 */

import { storedName } from '../mappers/settingNames';
import { asNumber, asText } from '../types/bpmn';

/** How the property panel shows one advanced setting. */
export type AdvancedVisibility = 'edit' | 'summary' | 'hidden';

/** What the panel says under a setting that basic mode shows read-only. */
export const CHANGE_IN_EXPERT_MODE = 'Turn on Expert mode to change it.';

export function advancedVisibility(expert: boolean, value: unknown): AdvancedVisibility {
  if (expert) return 'edit';
  return isSet(value) ? 'summary' : 'hidden';
}

/**
 * Whether a stored setting holds anything.
 *
 * The empty values are the ones the server leaves out when it stores a
 * definition: nothing, an empty string, zero, false, and an empty list or
 * object. Everything else counts, a string of spaces included, because the
 * engine reads it as it is.
 */
function isSet(value: unknown): boolean {
  if (value === undefined || value === null) return false;
  if (typeof value === 'string') return value !== '';
  if (typeof value === 'number') return value !== 0 && !Number.isNaN(value);
  if (typeof value === 'boolean') return value;
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === 'object') return Object.keys(value).length > 0;
  return true;
}

const PACE: ReadonlyMap<string, string> = new Map([
  ['parallel', 'all at the same time'],
  ['sequential', 'one after another'],
]);

/**
 * What a step's loop settings make it do, in a sentence, or undefined when it
 * runs once.
 *
 * Read the way the engine reads them. A step repeats for any multi-instance
 * type except an empty one and "none", so a type this editor does not offer,
 * from an imported file, is still a loop in effect. A list to go through wins
 * over a fixed count. A completion condition, once set, is what ends the loop,
 * in place of every run having finished.
 */
export function loopSummary(data: Record<string, unknown>): string | undefined {
  const type = asText(data.multiInstanceType);
  if (type === '' || type === 'none') return undefined;

  const collection = asText(data.collection);
  const count = asNumber(data.loopCardinality);
  const times = collection !== '' ? `once for each item in ${collection}` : timesFor(count);
  if (times === undefined) return 'Set to repeat, but it names no list and no count, so it runs once.';

  const pace = PACE.get(type);
  const sentences = [
    pace === undefined
      ? `Set to repeat ${times} as "${type}", which this editor does not recognise.`
      : `Runs ${times}, ${pace}.`,
  ];
  const itemName = asText(data.elementVariable);
  if (collection !== '' && itemName !== '') sentences.push(`Each run sees its item as ${itemName}.`);
  const doneWhen = asText(data.completionCondition);
  if (doneWhen !== '') sentences.push(`Moves on once this is true: ${doneWhen}.`);
  return sentences.join(' ');
}

function timesFor(count: number): string | undefined {
  if (count <= 0) return undefined;
  return count === 1 ? 'once' : `${count} times`;
}

/** One way a service task can do its work. */
export interface ImplementationOption {
  value: string;
  label: string;
  description: string;
}

const BASIC_IMPLEMENTATIONS: readonly ImplementationOption[] = [
  { value: 'push', label: 'Call a web address', description: 'We send the request and wait for the answer' },
  { value: 'connector', label: 'Use a connector', description: 'Slack, email and the rest, already set up' },
  { value: 'external', label: 'Let a worker pick it up', description: 'Your own program asks for work and reports back' },
];

const EXPERT_IMPLEMENTATIONS: readonly ImplementationOption[] = [
  { value: 'script', label: 'Run a script here', description: 'A little JavaScript, sandboxed' },
];

/**
 * The ways a service task can be set to work, as its select offers them.
 *
 * The step's current way is always among them. Basic mode leaves out the
 * expert ones, and a select with no option for its value draws an empty box,
 * which made a script step look as if it called nothing.
 */
export function implementationOptions(expert: boolean, current: string): ImplementationOption[] {
  const offered = expert ? [...BASIC_IMPLEMENTATIONS, ...EXPERT_IMPLEMENTATIONS] : [...BASIC_IMPLEMENTATIONS];
  if (current === '' || offered.some((option) => option.value === current)) return offered;
  return [...offered, optionFor(current)];
}

function optionFor(value: string): ImplementationOption {
  const known = EXPERT_IMPLEMENTATIONS.find((option) => option.value === value);
  if (known) return known;
  // From an imported file, or one saved by a later version. It is shown under
  // its own name rather than dropped.
  return { value, label: value, description: 'Set outside this editor' };
}

/** The dimmed lines under an item's name in a list. */
export interface ListDetails {
  detail?: string;
  id?: string;
}

/**
 * What a list shows under an item's name: what it belongs to, and in expert
 * mode its ID as well.
 *
 * Expert mode adds a line and never takes one's place. The project list showed
 * a project's ID instead of its organization, so turning it on hid which
 * organization each project was in.
 */
export function listDetails(expert: boolean, detail: string | undefined, id: string): ListDetails {
  return {
    ...(detail ? { detail } : {}),
    ...(expert ? { id } : {}),
  };
}

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
