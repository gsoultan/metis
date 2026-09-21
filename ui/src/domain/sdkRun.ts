/**
 * Which step the run is on, and what code that step would run.
 *
 * Pulled out of the page because none of it needs React: given the state of a
 * run, there is exactly one right answer for how far it has got and which call
 * the code panel should print. That makes it the kind of thing this repository
 * tests — and the code panel is worth testing, because a snippet showing the
 * wrong call is worse than showing none.
 */
import type { SdkCall } from './sdkCalls';
import { parseVariables } from './variablesInput';
import type { SandboxForm, LockedTask } from '../hooks/useSdkSandbox';

/** The step whose code the right-hand panel is showing. */
export type Focus = 'start' | 'work' | 'notify' | 'watch';

export type StepName = 'choose' | 'start' | 'work' | 'notify' | 'watch';

/** 'waiting' — its turn has not come · 'active' — you can act · 'done' — proven. */
export type StepState = 'waiting' | 'active' | 'done';

export function stepStates(instanceId: string | null, definitionKey: string): Record<StepName, StepState> {
  const chosen = definitionKey !== '';
  const started = instanceId !== null;
  return {
    choose: chosen ? 'done' : 'active',
    start: started ? 'done' : chosen ? 'active' : 'waiting',
    work: started ? 'active' : 'waiting',
    notify: started ? 'active' : 'waiting',
    watch: started ? 'active' : 'waiting',
  };
}

/**
 * Which step the code panel follows when nobody has clicked into one.
 *
 * A process with no external-task steps has nothing under "work", so following
 * it there would leave the panel empty on a page that is working perfectly.
 */
export function defaultFocus(run: { started: boolean; holdingTask: boolean; hasTopics: boolean }): Focus {
  if (run.holdingTask) return 'work';
  if (run.started) return (run.hasTopics ? 'work' : 'notify');
  return 'start';
}

/**
 * The call the code panel shows.
 *
 * Built from what is on screen rather than from what was last sent, so the code
 * is readable *before* the button is pressed — which is the order somebody
 * learning an API wants: read it, run it, take it away.
 *
 * Half-typed JSON is treated as no variables rather than as an error. The
 * variables box reports its own problem; making the snippet vanish mid-keystroke
 * would just make the panel flicker.
 */
export function previewCall(
  focus: Focus,
  projectId: string,
  form: SandboxForm,
  lockedTask: LockedTask | null,
): SdkCall | null {
  switch (focus) {
    case 'start':
      if (form.definitionKey === '') return null;
      return {
        kind: 'start',
        projectId,
        definitionKey: form.definitionKey,
        variables: variablesOf(form.startVariables),
        version: form.version,
        idempotencyKey: blankToUndefined(form.idempotencyKey),
      };

    case 'work':
      if (lockedTask !== null) {
        return {
          kind: 'complete',
          taskId: lockedTask.id,
          workerId: form.workerId,
          topic: lockedTask.topic,
          variables: variablesOf(form.completeVariables),
        };
      }
      if (form.topic === '') return null;
      return {
        kind: 'fetchAndLock',
        topic: form.topic,
        workerId: form.workerId,
        maxTasks: form.maxTasks,
        lockDurationMs: form.lockDurationMs,
      };

    case 'notify': {
      const name = form.notifyName.trim();
      if (name === '') return null;
      return form.notifyKind === 'message'
        ? {
            kind: 'message',
            projectId,
            messageName: name,
            correlationKey: form.correlationKey.trim(),
            variables: variablesOf(form.notifyVariables),
          }
        : {
            kind: 'signal',
            projectId,
            signalName: name,
            variables: variablesOf(form.notifyVariables),
          };
    }

    case 'watch':
      // Watching is a read the page does for you; there is no call to copy.
      return null;
  }
}

function variablesOf(text: string) {
  const parsed = parseVariables(text);
  return parsed.ok ? parsed.value : {};
}

function blankToUndefined(value: string): string | undefined {
  return value.trim() === '' ? undefined : value.trim();
}
