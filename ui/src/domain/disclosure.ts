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
