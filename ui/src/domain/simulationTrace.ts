/**
 * A simulation run, and everything the screen asks of it.
 *
 * The server returns the *whole* trace in one response rather than a step at a
 * time. That is the decision this file is built on: stepping, scrubbing and
 * stepping back are then array indexing, so the transport never waits on the
 * network and "go back one step" is correct by construction rather than by
 * replaying state. It also means none of this needs React, which is why it is
 * here and tested rather than inside a component.
 *
 * Every selector takes the run and an index and answers from first principles —
 * no accumulated state, no mutation. Scrub to step 7, then back to 2, and the
 * canvas at 2 is identical to the first time it was there.
 */
import type { ProcessVariables } from '../services/types';

/**
 * How the run finished.
 *
 * `needs_answer` is the one that shapes the screen. The engine reached a step
 * the outside world has to answer for — a person's task, a service call, a
 * message — and nobody has said what it does. That is not an error: it is the
 * process asking a question, and answering it in place on the diagram is how a
 * scenario gets built. Running an empty scenario and following the questions is
 * the intended path, not a fallback.
 */
export type SimulationOutcome = 'completed' | 'needs_answer' | 'incident' | 'step_budget' | 'timeout';

/** What a node is doing at a given step, as the canvas draws it. */
export type NodeSimState = 'pending' | 'active' | 'done' | 'failed';

/** Whether a sequence flow carried a token, so untaken branches can recede. */
export type FlowSimState = 'taken' | 'untaken';

/**
 * What a gateway or decision table decided, recorded at the step it decided.
 *
 * The plan asks for "watch which gateway and which DMN rule fires", and this is
 * that, as data: the expression as authored, what it evaluated to, and the flow
 * it chose. A gateway that chose nothing leaves `chose` unset and raises an
 * incident — it never silently picks a branch.
 */
export interface SimulationDecision {
  /** The condition as the author wrote it, e.g. `amount > 5000`. */
  expression: string;
  /** What it came to, rendered for reading: `false`, `"APPROVED"`. */
  result: string;
  /** The sequence flow taken. Absent when nothing matched. */
  chose?: string;
  /** For a business rule task: which table and which rule fired. */
  rule?: string;
}

/**
 * A refusal the engine raised.
 *
 * Present in a simulation for the same reason it is present in production: a
 * decision point that matched nothing is an incident, never a fallback. A
 * simulation that hid this would certify a broken model, which is worse than no
 * simulation.
 */
export interface SimulationIncident {
  node: string;
  message: string;
  /** What to do about it, when the server can say. */
  hint?: string;
}

export interface SimulationStep {
  /** Position in the trace. The transport's cursor indexes on this. */
  i: number;
  /** Virtual time, ISO-8601. Not wall-clock — a `PT24H` timer moves it a day. */
  clock: string;
  /** The flow node this step concerns. */
  node: string;
  event: 'started' | 'entered' | 'left' | 'waiting' | 'decided' | 'incident' | 'ended';
  /** Node ids holding a token *after* this step. Parallel paths hold several. */
  tokens: string[];
  /** The sequence flow followed to reach this node, when one was. */
  flow?: string;
  /** Only the variables this step changed, so the panel can mark them. */
  variablesDelta?: ProcessVariables;
  /** Plain English, written for somebody who does not read node ids. */
  note: string;
  decision?: SimulationDecision;
  incident?: SimulationIncident;
}

export interface SimulationRun {
  runId: string;
  definition: { key: string; version: number; id: string };
  outcome: SimulationOutcome;
  /** Where it stopped. Null when it never reached an end event. */
  endedAtNode: string | null;
  /**
   * The node whose answer the run is waiting for, when `outcome` is
   * `needs_answer`. This is what the screen points at.
   */
  awaitingNode: string | null;
  /** What that node is called, so the question can be asked in plain words. */
  awaitingLabel: string | null;
  /** Virtual time elapsed across the whole run. */
  virtualDurationMs: number;
  /** The variables as the run left them. */
  variables: ProcessVariables;
  incidents: SimulationIncident[];
  steps: SimulationStep[];
}

/** The last index the transport can move to. -1 for a run with no steps. */
export function lastIndex(run: SimulationRun): number {
  return run.steps.length - 1;
}

/**
 * Keep a cursor inside the trace.
 *
 * Clamped rather than rejected: the transport calls this on every button, and a
 * "step forward" at the end should sit still, not throw.
 */
export function clampIndex(run: SimulationRun, index: number): number {
  const max = lastIndex(run);
  if (max < 0) return 0;
  if (index < 0) return 0;
  return index > max ? max : index;
}

/**
 * What every node is doing at `index`.
 *
 * Read forwards from the start each time rather than kept as running state —
 * that is what makes scrubbing backwards give the same answer as arriving
 * forwards. Traces are bounded by the server's step budget, so the cost is a
 * few hundred iterations on a canvas that is about to repaint anyway.
 *
 * A node that has been entered and left is `done`; one holding a token is
 * `active`; one that raised an incident is `failed` and stays that way, because
 * the run stopped there and "it recovered" would be a lie.
 */
export function nodeStatesAt(run: SimulationRun, index: number): Record<string, NodeSimState> {
  const states: Record<string, NodeSimState> = {};
  const upto = clampIndex(run, index);

  for (let i = 0; i <= upto && i < run.steps.length; i += 1) {
    const step = run.steps[i];
    if (step.event === 'incident') {
      states[step.node] = 'failed';
      continue;
    }
    // A node that already failed keeps its verdict; nothing later un-fails it.
    if (states[step.node] === 'failed') continue;

    if (step.event === 'left' || step.event === 'ended') {
      states[step.node] = 'done';
    } else {
      states[step.node] = 'active';
    }
  }

  // The tokens the engine reports are the truth about what is live right now.
  // A node marked done earlier can hold a token again on a second pass through
  // a loop, and the token list is what says so.
  const live = run.steps[upto]?.tokens ?? [];
  for (const node of live) {
    if (states[node] !== 'failed') {
      states[node] = 'active';
    }
  }

  return states;
}

/**
 * Which sequence flows carried a token by `index`.
 *
 * Only the taken ones are listed. The canvas fades everything absent, which is
 * the whole point: a designer asking "which branch fired?" should be able to
 * answer it without reading a log.
 */
export function flowStatesAt(run: SimulationRun, index: number): Record<string, FlowSimState> {
  const states: Record<string, FlowSimState> = {};
  const upto = clampIndex(run, index);

  for (let i = 0; i <= upto && i < run.steps.length; i += 1) {
    const flow = run.steps[i].flow;
    if (flow !== undefined) {
      states[flow] = 'taken';
    }
  }

  return states;
}

/**
 * The variables as they stood at `index`.
 *
 * Accumulated from the deltas, so the panel can show the state at any point in
 * the run rather than only at the end. A key set back to the same value still
 * counts as set — the engine said it wrote it, and hiding that would make a
 * no-op assignment invisible while debugging one.
 */
export function variablesAt(run: SimulationRun, index: number): ProcessVariables {
  const merged: ProcessVariables = {};
  const upto = clampIndex(run, index);

  for (let i = 0; i <= upto && i < run.steps.length; i += 1) {
    const delta = run.steps[i].variablesDelta;
    if (delta === undefined) continue;
    for (const key of Object.keys(delta)) {
      merged[key] = delta[key];
    }
  }

  return merged;
}

/** The names this step wrote, so the variables panel can mark them as changed. */
export function changedKeysAt(run: SimulationRun, index: number): string[] {
  const step = run.steps[clampIndex(run, index)];
  const delta = step?.variablesDelta;
  return delta === undefined ? [] : Object.keys(delta);
}

/** Virtual milliseconds elapsed from the first step to `index`. */
export function elapsedMsAt(run: SimulationRun, index: number): number {
  if (run.steps.length === 0) return 0;
  const start = Date.parse(run.steps[0].clock);
  const at = Date.parse(run.steps[clampIndex(run, index)].clock);
  if (Number.isNaN(start) || Number.isNaN(at)) return 0;
  return at - start;
}

/** The virtual clock reading at `index`, or null when the run has no steps. */
export function clockAt(run: SimulationRun, index: number): Date | null {
  if (run.steps.length === 0) return null;
  const parsed = Date.parse(run.steps[clampIndex(run, index)].clock);
  return Number.isNaN(parsed) ? null : new Date(parsed);
}

/**
 * Elapsed virtual time, written the way somebody discusses a process.
 *
 * "1d 0h 14m", not "86040000ms" and not "a day ago". A process person reasons
 * in days and hours because that is what an SLA is written in, and the whole
 * reason the clock is on screen is that a `PT24H` timer should read as a day
 * passing rather than as a step that took no time.
 */
export function describeElapsed(ms: number): string {
  if (ms <= 0) return '0m';

  const minutes = Math.floor(ms / 60_000) % 60;
  const hours = Math.floor(ms / 3_600_000) % 24;
  const days = Math.floor(ms / 86_400_000);

  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0 || days > 0) parts.push(`${hours}h`);
  parts.push(`${minutes}m`);
  return parts.join(' ');
}

/**
 * Which day of the run this is, counted from the start.
 *
 * Day 1 is the day it started, because that is how people count days of a
 * process — an expense submitted on Monday and approved on Tuesday took two
 * days, not one.
 */
export function dayNumber(run: SimulationRun, index: number): number {
  return Math.floor(elapsedMsAt(run, index) / 86_400_000) + 1;
}

/** `09:14`, in the run's own clock, for the transport bar. */
export function timeOfDay(run: SimulationRun, index: number): string {
  const at = clockAt(run, index);
  if (at === null) return '--:--';
  const hh = String(at.getUTCHours()).padStart(2, '0');
  const mm = String(at.getUTCMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

/** The whole clock readout: `Day 2, 09:14 (+1d 0h 14m)`. */
export function describeClock(run: SimulationRun, index: number): string {
  if (run.steps.length === 0) return 'No run yet';
  const elapsed = elapsedMsAt(run, index);
  const suffix = elapsed > 0 ? ` (+${describeElapsed(elapsed)})` : '';
  return `Day ${dayNumber(run, index)}, ${timeOfDay(run, index)}${suffix}`;
}

/**
 * Where two runs of the same scenario first disagree.
 *
 * This is the question a version comparison exists to answer — not "are they
 * different" but "where, and what does that mean for this case". Compared on
 * the node sequence rather than on the steps wholesale, because timestamps
 * differ whenever a duration was estimated and a diff that reports every step
 * as changed reports nothing.
 *
 * Null when the two took the same path, which is the answer that lets somebody
 * deploy.
 */
export function firstDivergence(
  a: SimulationRun,
  b: SimulationRun,
): { index: number; aNode: string; bNode: string } | null {
  const shared = Math.min(a.steps.length, b.steps.length);

  for (let i = 0; i < shared; i += 1) {
    if (a.steps[i].node !== b.steps[i].node) {
      return { index: i, aNode: a.steps[i].node, bNode: b.steps[i].node };
    }
  }

  if (a.steps.length === b.steps.length) return null;

  // One ran longer. The first extra step is the divergence, and the shorter run
  // has nothing there — which is itself the finding ("v3 stopped here").
  const longer = a.steps.length > b.steps.length ? a : b;
  const extra = longer.steps[shared];
  return {
    index: shared,
    aNode: a.steps.length > shared ? extra.node : '—',
    bNode: b.steps.length > shared ? extra.node : '—',
  };
}

/**
 * Did the run reach this node? The assertion the SDK exposes, available here so
 * the scenario list can show pass/fail without re-deriving it.
 */
export function reached(run: SimulationRun, nodeId: string): boolean {
  return run.steps.some((step) => step.node === nodeId);
}

/** A run is a pass when it completed and raised nothing. */
export function passed(run: SimulationRun): boolean {
  return run.outcome === 'completed' && run.incidents.length === 0;
}

/**
 * How a run ended, as the one line the screen leads with.
 *
 * `tone` rather than a colour, so the component decides how to render it and
 * this stays testable. Four tones, and the difference between two of them is
 * the whole design: `asking` is the process needing something, which is normal
 * and has an obvious next action; `bad` is the model being wrong, which is the
 * finding somebody came here for. Collapsing those two into "error" is what
 * makes a simulator feel like it is failing when it is working.
 */
export interface Verdict {
  tone: 'good' | 'asking' | 'bad' | 'warn';
  /** One sentence, in business words. */
  headline: string;
  /** What to do about it, when there is something to say. */
  detail?: string;
  /** The node to point at on the diagram. */
  node?: string;
}

export function verdict(run: SimulationRun): Verdict {
  if (run.outcome === 'needs_answer' && run.awaitingNode !== null) {
    const name = run.awaitingLabel ?? run.awaitingNode;
    return {
      tone: 'asking',
      headline: `Waiting at ${name}`,
      detail: 'Nothing has said what happens here. Answer it on the diagram and the run carries on.',
      node: run.awaitingNode,
    };
  }

  if (run.incidents.length > 0) {
    const incident = run.incidents[0];
    return {
      tone: 'bad',
      headline: incident.message,
      detail:
        incident.hint ??
        'A real run would raise an incident here and the case would stop. It never picks a branch on its own.',
      node: incident.node,
    };
  }

  switch (run.outcome) {
    case 'completed':
      return {
        tone: 'good',
        headline: run.endedAtNode === null ? 'Finished' : `Finished at ${run.endedAtNode}`,
        detail: 'Nothing was raised along the way.',
      };
    case 'step_budget':
      return {
        tone: 'warn',
        headline: 'Stopped at the step limit',
        detail: 'This usually means a loop with no way out. Check the gateway that sends work backwards.',
      };
    case 'timeout':
      return { tone: 'warn', headline: 'Took too long to simulate', detail: 'Try a smaller case.' };
    default:
      return { tone: 'bad', headline: 'Stopped on an incident' };
  }
}

/** The same verdict as one line, for a list of cases. */
export function describeOutcome(run: SimulationRun): string {
  return verdict(run).headline;
}
