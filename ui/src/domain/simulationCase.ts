/**
 * A case worth keeping: a scenario, saved with its process, and what it must
 * end with.
 *
 * A scenario on its own answers "what does this process do with £4,200?". A
 * case with an expectation answers the question that matters after the next
 * edit: "does it *still* do that?". So a case says how the run must end, where,
 * and holding which values — and a run either matches all of it or names, in
 * words, each thing it got wrong.
 *
 * Nothing here is filled in by hand first. "Expect this result" copies the run
 * that is on screen, and the person deletes what they do not care about. The
 * same rule as the scenario: nobody fills in a form before seeing the process
 * do something.
 */
import { formatValue, variablesOf, type Scenario, type SimulationAnswer, type VariableRow } from './simulationScenario';
import type { SimulationRun, Verdict } from './simulationTrace';
import type { ProcessVariables } from '../services/types';

/** The outcomes a case may expect. Running out of steps is never a result. */
export type ExpectedOutcome = '' | 'completed' | 'needs_answer' | 'incident';

export interface Expectation {
  /** Empty means "however it ends" — only the rest is checked. */
  outcome: ExpectedOutcome;
  /** Where a completed run ends, or where an unanswered one stops. */
  endsAt: string;
  /** Values the run must end holding. */
  variables: VariableRow[];
}

export function blankExpectation(): Expectation {
  return { outcome: '', endsAt: '', variables: [] };
}

export function hasExpectation(expect: Expectation | undefined): expect is Expectation {
  return expect !== undefined && (expect.outcome !== '' || expect.endsAt !== '' || expect.variables.length > 0);
}

/** Where a run finished, or stopped to ask. */
function endOf(run: SimulationRun): string {
  if (run.outcome === 'needs_answer') return run.awaitingNode ?? '';
  return run.endedAtNode ?? '';
}

function expectableOutcome(run: SimulationRun): ExpectedOutcome {
  switch (run.outcome) {
    case 'completed':
    case 'needs_answer':
    case 'incident':
      return run.outcome;
    default:
      return '';
  }
}

/**
 * The run on screen, as an expectation.
 *
 * The outcome and where it ended, but no variables: a run ends holding every
 * value the process ever touched, and expecting all of them makes a case fail
 * on a timestamp. The person adds the few that matter.
 */
export function expectationFromRun(run: SimulationRun): Expectation {
  return { outcome: expectableOutcome(run), endsAt: endOf(run), variables: [] };
}

export interface ExpectationCheck {
  passed: boolean;
  /** Each thing the run got wrong, in words. Empty when it passed. */
  misses: string[];
}

const OUTCOME_WORDS: Record<Exclude<ExpectedOutcome, ''>, string> = {
  completed: 'finish',
  needs_answer: 'stop to ask for an answer',
  incident: 'raise an incident',
};

/** Two values are the same when they serialise the same, with keys sorted. */
function sameValue(a: unknown, b: unknown): boolean {
  const canonical = (value: unknown) =>
    JSON.stringify(value, (_key: string, inner: unknown) =>
      inner !== null && typeof inner === 'object' && !Array.isArray(inner)
        ? Object.fromEntries(Object.entries(inner).sort(([x], [y]) => x.localeCompare(y)))
        : inner,
    );
  return canonical(a) === canonical(b);
}

/**
 * Whether a run is what its case expects.
 *
 * `label` turns a node id into what the diagram calls it, so a miss reads
 * "ended at Rejected, not Approved" rather than naming two element ids.
 */
export function checkExpectation(
  run: SimulationRun,
  expect: Expectation,
  label: (nodeId: string) => string = (nodeId) => nodeId,
): ExpectationCheck {
  const misses: string[] = [];

  if (expect.outcome !== '' && run.outcome !== expect.outcome) {
    misses.push(`It was expected to ${OUTCOME_WORDS[expect.outcome]}, and did not.`);
  }

  const end = endOf(run);
  if (expect.endsAt !== '' && end !== expect.endsAt) {
    misses.push(
      end === ''
        ? `It was expected to end at ${label(expect.endsAt)}, and never got there.`
        : `It ended at ${label(end)}, not ${label(expect.endsAt)}.`,
    );
  }

  for (const [name, want] of Object.entries(variablesOf(expect.variables))) {
    if (!(name in run.variables)) {
      misses.push(`${name} was expected to be ${formatValue(want)}, and was never set.`);
    } else if (!sameValue(run.variables[name], want)) {
      misses.push(`${name} is ${formatValue(run.variables[name])}, not ${formatValue(want)}.`);
    }
  }

  return { passed: misses.length === 0, misses };
}

/* ── The wire ─────────────────────────────────────────────────────────────── */

type WireCaseAnswer = {
  node: string;
  kind: SimulationAnswer['kind'];
  actor?: string;
  after?: string;
  returns?: ProcessVariables;
  fails?: string;
  arrives_after?: string;
};

/** A case as `/projects/{id}/simulation-cases/{key}` stores it. */
export interface WireCase {
  id?: string;
  name: string;
  scenario: { variables: ProcessVariables; answers: WireCaseAnswer[]; seed: number };
  expect: { outcome?: ExpectedOutcome; ends_at?: string; variables?: ProcessVariables };
}

function answerToWire(answer: SimulationAnswer): WireCaseAnswer {
  switch (answer.kind) {
    case 'person':
      return { node: answer.nodeId, kind: 'person', actor: answer.actor, after: answer.after };
    case 'message':
      return { node: answer.nodeId, kind: 'message', arrives_after: answer.arrivesAfter };
    case 'service':
      return answer.outcome === 'fail'
        ? { node: answer.nodeId, kind: 'service', fails: answer.failureCode }
        : { node: answer.nodeId, kind: 'service', returns: variablesOf(answer.returns) };
  }
}

let rowCounter = 0;

/** Rows for a map, with ids stable enough for React keys within one load. */
function rowsOf(values: ProcessVariables | undefined): VariableRow[] {
  return Object.entries(values ?? {}).map(([name, value]) => ({
    id: `row-${++rowCounter}`,
    name,
    value: formatValue(value),
  }));
}

function answerFromWire(answer: WireCaseAnswer): SimulationAnswer {
  switch (answer.kind) {
    case 'person':
      return { nodeId: answer.node, kind: 'person', actor: answer.actor ?? '', after: answer.after ?? '' };
    case 'message':
      return { nodeId: answer.node, kind: 'message', arrivesAfter: answer.arrives_after ?? '' };
    case 'service':
      return {
        nodeId: answer.node,
        kind: 'service',
        outcome: answer.fails !== undefined && answer.fails !== '' ? 'fail' : 'succeed',
        returns: rowsOf(answer.returns),
        failureCode: answer.fails ?? '',
      };
  }
}

export function toWireCase(scenario: Scenario): WireCase {
  const expect = scenario.expect ?? blankExpectation();
  return {
    name: scenario.name.trim() === '' ? 'Untitled case' : scenario.name.trim(),
    scenario: {
      variables: variablesOf(scenario.variables),
      answers: scenario.answers.map(answerToWire),
      seed: scenario.seed,
    },
    expect: {
      outcome: expect.outcome === '' ? undefined : expect.outcome,
      ends_at: expect.endsAt === '' ? undefined : expect.endsAt,
      variables: expect.variables.length === 0 ? undefined : variablesOf(expect.variables),
    },
  };
}

/** A saved case as an editable scenario. Its local id is its saved id. */
export function fromWireCase(wire: WireCase & { id: string }): Scenario {
  return {
    id: wire.id,
    savedId: wire.id,
    name: wire.name,
    variables: rowsOf(wire.scenario.variables),
    answers: (wire.scenario.answers ?? []).map(answerFromWire),
    seed: wire.scenario.seed,
    expect: {
      outcome: wire.expect.outcome ?? '',
      endsAt: wire.expect.ends_at ?? '',
      variables: rowsOf(wire.expect.variables),
    },
  };
}

/**
 * What a case would save as. Two scenarios with the same fingerprint save the
 * same, which is how the rail knows a saved case has changed since.
 */
export function caseFingerprint(scenario: Scenario): string {
  return JSON.stringify(toWireCase(scenario));
}

/**
 * Whether a run passes its case: what the case expects when it expects
 * anything, and otherwise the old rule — it finished and raised nothing.
 */
export function casePassed(scenario: Scenario, run: SimulationRun, label?: (nodeId: string) => string): boolean {
  if (hasExpectation(scenario.expect)) return checkExpectation(run, scenario.expect, label).passed;
  return run.outcome === 'completed' && run.incidents.length === 0;
}

/** A check as the one line the rail leads with. */
export function expectationVerdict(check: ExpectationCheck): Verdict {
  if (check.passed) {
    return { tone: 'good', headline: 'This is what the case expects' };
  }
  return {
    tone: 'bad',
    headline: check.misses.length === 1 ? 'Not what this case expects' : `${check.misses.length} things differ from what this case expects`,
    detail: check.misses.join(' '),
  };
}
