/** The ways a service task can be set to work, as the property panel offers them. */

/** One way a service task can do its work. */
export interface ImplementationOption {
  value: string;
  label: string;
  description: string;
}

const IMPLEMENTATIONS: readonly ImplementationOption[] = [
  { value: 'push', label: 'Call a web address', description: 'We send the request and wait for the answer' },
  { value: 'connector', label: 'Use a connector', description: 'Slack, email and the rest, already set up' },
  { value: 'external', label: 'Let a worker pick it up', description: 'Your own program asks for work and reports back' },
];

/**
 * A step saved as running a script. The engine runs a script only on a script
 * step: on this kind it was skipped as if it called nothing, and deploy refuses
 * it now. It is still shown, so the person can see why and move off it.
 */
const SCRIPT: ImplementationOption = {
  value: 'script',
  label: 'Run a script (this step cannot)',
  description: 'A step that calls another system cannot run a script. Move it to a script step, or choose another way.',
};

/**
 * The ways a service task can be set to work, as its select offers them.
 *
 * The step's current way is always among them: a select with no option for
 * its value draws an empty box, which reads as a step that calls nothing.
 */
export function implementationOptions(current: string): ImplementationOption[] {
  const offered = [...IMPLEMENTATIONS];
  if (current === '' || offered.some((option) => option.value === current)) return offered;
  return [...offered, optionFor(current)];
}

/**
 * Whether the panel lets the person change how a service task works.
 *
 * Basic mode chooses among the ways it offers, and shows a step set another
 * way, from outside this editor, read-only: changing it there could drop a
 * setting basic mode cannot show. A step set to run a script is the exception.
 * That script cannot run here, so moving off it is the fix, in either mode.
 */
export function canChooseImplementation(expert: boolean, current: string): boolean {
  return expert || current === SCRIPT.value || IMPLEMENTATIONS.some((option) => option.value === current);
}

function optionFor(value: string): ImplementationOption {
  if (value === SCRIPT.value) return SCRIPT;
  // From an imported file, or one saved by a later version. It is shown under
  // its own name rather than dropped.
  return { value, label: value, description: 'Set outside this editor' };
}
