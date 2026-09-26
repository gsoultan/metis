/** The ways a service task can be set to work, as the property panel offers them in each mode. */

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

/**
 * Whether the panel lets the person change how a service task works.
 *
 * Basic mode chooses among the ways it offers. A step set another way, to run
 * a script or in a way from outside this editor, is shown read-only there:
 * the choice used to stay live, and choosing "Call a web address" on a script
 * step dropped the script with no way back, since basic mode offers none.
 */
export function canChooseImplementation(expert: boolean, current: string): boolean {
  return expert || BASIC_IMPLEMENTATIONS.some((option) => option.value === current);
}

function optionFor(value: string): ImplementationOption {
  const known = EXPERT_IMPLEMENTATIONS.find((option) => option.value === value);
  if (known) return known;
  // From an imported file, or one saved by a later version. It is shown under
  // its own name rather than dropped.
  return { value, label: value, description: 'Set outside this editor' };
}
