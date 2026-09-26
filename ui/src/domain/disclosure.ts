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
