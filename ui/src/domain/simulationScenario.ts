/**
 * A scenario — the case a process is expected to get right — and the request
 * that runs it.
 *
 * Shaped around one rule: **nobody fills in a form before pressing Run.** The
 * first version of this asked for a JSON payload, a list of stubs each naming a
 * node you had to remember the id of, a seed and a step budget — seven controls
 * before anything could happen, most of them meaningless until you had seen the
 * process run once.
 *
 * So a scenario starts empty and runs. Where the engine needs something the
 * outside world would have provided, it stops and says which node it stopped
 * at, and that node is answered in place on the diagram. The scenario fills
 * itself in, one question at a time, in the order the process actually asks.
 *
 * Two things read this file: the Run button, and the panel that prints the code
 * to run the same scenario from CI. They read the *same* request object, for
 * the reason `sdkCalls.ts` gives — a second hand-maintained description of the
 * call drifts, and an example that does not match what just ran is worse than
 * no example at all.
 */
import type { ProcessVariables } from '../services/types';

/**
 * One named value, as a row rather than a line of JSON.
 *
 * `amount` `4200` is what somebody testing an expense process wants to type.
 * The value is kept as text and interpreted on the way out, so `4200` is a
 * number, `EU` is a string and neither needs quoting rules explained. Anyone
 * who does need nested objects can still type JSON into the value and it is
 * read as JSON — the escape hatch is there without being in the way.
 */
export interface VariableRow {
  id: string;
  name: string;
  value: string;
}

/**
 * What the outside world says when the process asks it something.
 *
 * Keyed by node, because that is how it is authored: you click the step on the
 * diagram that is waiting and answer *it*. There is no list of stubs to keep in
 * sync with the model, and no node id to remember.
 *
 * Rolling the database back protects the audit trail; it does not un-send an
 * HTTP request. So these are not an optimisation — they are the only reason a
 * simulation cannot charge a real card.
 */
export type SimulationAnswer =
  | { nodeId: string; kind: 'person'; actor: string; after: string }
  | { nodeId: string; kind: 'service'; outcome: 'succeed' | 'fail'; returns: VariableRow[]; failureCode: string }
  | { nodeId: string; kind: 'message'; arrivesAfter: string };

export type AnswerKind = SimulationAnswer['kind'];

export interface Scenario {
  id: string;
  /** What this case is called, in business terms: "Over limit, no manager". */
  name: string;
  variables: VariableRow[];
  answers: SimulationAnswer[];
  /**
   * Fixes every estimated duration, so the same scenario gives the same trace
   * every time. Expert-only: it is never shown until somebody opens Advanced,
   * because the default is right for everybody who is not writing a CI gate.
   */
  seed: number;
}

/**
 * What is being simulated.
 *
 * Three modes, because simulation is asked for at three different moments:
 * before a definition is deployed, against one that is, and against a real
 * instance whose recorded variables answer "would the new version have helped?".
 */
export type SimulationTarget =
  | { kind: 'deployed'; projectId: string; definitionKey: string; version: number }
  | { kind: 'draft'; projectId: string; definitionKey: string; bpmnXml: string }
  | { kind: 'replay'; projectId: string; instanceId: string; definitionKey: string; version: number };

/* ── Durations, in the words people use ───────────────────────────────────── */

/**
 * BPMN durations are ISO-8601 — `PT24H`, not `24h` — and the engine is right to
 * insist. That is a wire format, though, not a thing to make somebody type. The
 * choices below cover almost every answer anybody gives, so the common path
 * never sees an ISO string at all, and `isIso8601Duration` is left guarding the
 * custom field for the rare case that does.
 */
export const DURATION_CHOICES: { value: string; label: string }[] = [
  { value: 'PT5M', label: '5 minutes' },
  { value: 'PT30M', label: '30 minutes' },
  { value: 'PT1H', label: '1 hour' },
  { value: 'PT4H', label: '4 hours' },
  { value: 'PT24H', label: '1 day' },
  { value: 'P2D', label: '2 days' },
  { value: 'P3D', label: '3 days' },
  { value: 'P7D', label: '1 week' },
];

/** `PT24H` → "1 day". Falls back to the ISO string for a custom value. */
export function describeDuration(iso: string): string {
  return DURATION_CHOICES.find((choice) => choice.value === iso)?.label ?? iso;
}

const ISO_DURATION = /^P(?!$)(\d+Y)?(\d+M)?(\d+W)?(\d+D)?(T(?!$)(\d+H)?(\d+M)?(\d+(\.\d+)?S)?)?$/;

export function isIso8601Duration(value: string): boolean {
  return ISO_DURATION.test(value.trim());
}

/* ── Values, without a JSON lesson ────────────────────────────────────────── */

/**
 * Read a typed value the way somebody meant it.
 *
 * `4200` is a number because an amount is a number; `true` is a boolean; `EU`
 * is a string without needing quotes. Anything starting with `{` or `[` is
 * tried as JSON, so nested payloads still work — but nobody has to know that
 * to type an amount.
 */
export function parseValue(text: string): unknown {
  const trimmed = text.trim();
  if (trimmed === '') return '';
  if (trimmed === 'true') return true;
  if (trimmed === 'false') return false;
  if (trimmed === 'null') return null;

  if (/^-?\d+(\.\d+)?$/.test(trimmed)) {
    return Number(trimmed);
  }

  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.parse(trimmed) as unknown;
    } catch {
      // Half-typed JSON is a string until it is finished. Reporting a parse
      // error on every keystroke is how a field stops being read.
      return trimmed;
    }
  }

  return trimmed;
}

/** The inverse, for showing a value that came back from a run. */
export function formatValue(value: unknown): string {
  if (typeof value === 'string') return value;
  return JSON.stringify(value) ?? '';
}

/** Rows to the object the wire carries. Unnamed rows are ignored, not an error. */
export function variablesOf(rows: VariableRow[]): ProcessVariables {
  const out: ProcessVariables = {};
  for (const row of rows) {
    const name = row.name.trim();
    if (name === '') continue;
    out[name] = parseValue(row.value) as ProcessVariables[string];
  }
  return out;
}

/* ── Building and editing a scenario ──────────────────────────────────────── */

export function emptyScenario(id: string, name = 'New case'): Scenario {
  return { id, name, variables: [], answers: [], seed: 1 };
}

/** The answer a given BPMN node needs, or null when it needs none. */
export function answerKindFor(nodeType: string | undefined): AnswerKind | null {
  switch (nodeType) {
    case 'userTask':
    case 'manualTask':
      return 'person';
    case 'serviceTask':
    case 'sendTask':
      return 'service';
    case 'intermediateCatchEvent':
    case 'receiveTask':
      return 'message';
    default:
      // Gateways, scripts and decision tables run for real — there is nothing
      // outside them to answer for, so clicking one offers nothing.
      return null;
  }
}

export function blankAnswer(nodeId: string, kind: AnswerKind): SimulationAnswer {
  switch (kind) {
    case 'person':
      return { nodeId, kind, actor: '', after: 'PT24H' };
    case 'service':
      return { nodeId, kind, outcome: 'succeed', returns: [], failureCode: '' };
    case 'message':
      return { nodeId, kind, arrivesAfter: 'PT1H' };
  }
}

export function answerFor(scenario: Scenario, nodeId: string): SimulationAnswer | null {
  return scenario.answers.find((answer) => answer.nodeId === nodeId) ?? null;
}

/** Set or replace the answer for a node. One answer per node, by construction. */
export function upsertAnswer(scenario: Scenario, answer: SimulationAnswer): Scenario {
  const others = scenario.answers.filter((existing) => existing.nodeId !== answer.nodeId);
  return { ...scenario, answers: [...others, answer] };
}

export function removeAnswer(scenario: Scenario, nodeId: string): Scenario {
  return { ...scenario, answers: scenario.answers.filter((answer) => answer.nodeId !== nodeId) };
}

/**
 * Is this scenario ready to send?
 *
 * Deliberately permissive. A scenario with no variables and no answers is
 * perfectly valid — running it is how you find out what it needs. The only
 * things refused are ones the server would reject outright, so the Run button
 * is almost never disabled and the process does the teaching.
 */
export function validateScenario(scenario: Scenario): string[] {
  const problems: string[] = [];

  for (const answer of scenario.answers) {
    if (answer.kind === 'person' && !isIso8601Duration(answer.after)) {
      problems.push(`"${answer.after}" is not a duration. Pick one from the list, or write it as PT24H.`);
    }
    if (answer.kind === 'message' && !isIso8601Duration(answer.arrivesAfter)) {
      problems.push(`"${answer.arrivesAfter}" is not a duration. Pick one from the list, or write it as PT2H.`);
    }
    if (answer.kind === 'service' && answer.outcome === 'fail' && answer.failureCode.trim() === '') {
      problems.push('A failing step needs an error code, so a boundary event can catch it.');
    }
  }

  return problems;
}

/* ── The wire ─────────────────────────────────────────────────────────────── */

export interface SimulationRequest {
  project_id: string;
  definition_key?: string;
  version?: number;
  bpmn_xml?: string;
  replay_instance_id?: string;
  variables: ProcessVariables;
  answers: WireAnswer[];
  /**
   * Where virtual time starts, ISO-8601.
   *
   * Sent rather than left to the server, so a case run twice gives the same
   * trace — which is the whole basis of asserting on one in CI. Timers always
   * fast-forward; there is no mode where a simulation waits, so there is no
   * flag for it.
   */
  clock_start: string;
  seed: number;
  max_steps: number;
}

type WireAnswer =
  | { node: string; kind: 'person'; actor: string; after: string }
  | { node: string; kind: 'service'; returns: ProcessVariables }
  | { node: string; kind: 'service'; fails: string }
  | { node: string; kind: 'message'; arrives_after: string };

/**
 * The step budget every run carries.
 *
 * A process with a loop the author did not intend would otherwise run until the
 * server's timeout, holding a transaction open the whole way. Stopping at a
 * known number and *saying so* turns a hang into a finding.
 */
export const DEFAULT_MAX_STEPS = 500;

export const SIMULATION_PATH = '/simulations';

/**
 * Where every simulated case starts, in virtual time.
 *
 * A fixed instant rather than "now": two runs of the same case have to produce
 * the same trace, and a clock that moves with the wall clock makes every
 * timestamp in a trace different from the last run's. The date itself carries
 * no meaning — it is a readable Monday morning.
 */
export const SIMULATION_CLOCK_START = '2026-01-05T09:00:00Z';

function toWire(answer: SimulationAnswer): WireAnswer {
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

/** The one description of the call. Both the Run button and the snippet read it. */
export function simulationRequest(scenario: Scenario, target: SimulationTarget): SimulationRequest {
  const base = {
    project_id: target.projectId,
    variables: variablesOf(scenario.variables),
    answers: scenario.answers.map(toWire),
    clock_start: SIMULATION_CLOCK_START,
    seed: scenario.seed,
    max_steps: DEFAULT_MAX_STEPS,
  };

  switch (target.kind) {
    case 'deployed':
      return { ...base, definition_key: target.definitionKey, version: target.version };
    case 'draft':
      return { ...base, definition_key: target.definitionKey, bpmn_xml: target.bpmnXml };
    case 'replay':
      return {
        ...base,
        definition_key: target.definitionKey,
        version: target.version,
        replay_instance_id: target.instanceId,
      };
  }
}

/* ── The same run, as code ────────────────────────────────────────────────── */

/**
 * A draft is deliberately given no snippet: there is nothing for CI to point at
 * until the definition is deployed, and handing somebody code with a wall of
 * inline XML in it would be a worse answer than saying so.
 */
export function goSnippet(request: SimulationRequest, scenario: Scenario): string | null {
  if (request.bpmn_xml !== undefined) return null;

  const vars = Object.entries(request.variables)
    .map(([key, value]) => `            ${JSON.stringify(key)}: ${goLiteral(value)},`)
    .join('\n');

  const answers = request.answers.map((answer) => `            ${goAnswer(answer)},`).join('\n');

  return `func Test${goIdentifier(scenario.name)}(t *testing.T) {
    run, err := client.Simulation().Run(ctx, metis.SimulationRequest{
        ProjectID:     ${JSON.stringify(request.project_id)},
        DefinitionKey: ${JSON.stringify(request.definition_key ?? '')},
        Version:       ${request.version ?? 0}, // 0 = whatever is live
        Variables: metis.Vars{
${vars}
        },
        Answers: []metis.Answer{
${answers}
        },
        Seed: ${request.seed},
    })
    if err != nil {
        t.Fatal(err)
    }

    metistest.AssertNoIncidents(t, run)
    metistest.AssertEndsAt(t, run, "EndEvent_Approved")
}`;
}

function goAnswer(answer: WireAnswer): string {
  if (answer.kind === 'message') {
    return `metis.Message(${JSON.stringify(answer.node)}).ArrivesAfter(${JSON.stringify(answer.arrives_after)})`;
  }
  if (answer.kind === 'person') {
    return `metis.Task(${JSON.stringify(answer.node)}).CompletedBy(${JSON.stringify(answer.actor)}).After(${JSON.stringify(answer.after)})`;
  }
  if ('fails' in answer) {
    return `metis.Service(${JSON.stringify(answer.node)}).Fails(${JSON.stringify(answer.fails)})`;
  }
  return `metis.Service(${JSON.stringify(answer.node)}).Returns(metis.Vars{${goVars(answer.returns)}})`;
}

function goVars(vars: ProcessVariables): string {
  return Object.entries(vars)
    .map(([key, value]) => `${JSON.stringify(key)}: ${goLiteral(value)}`)
    .join(', ');
}

function goLiteral(value: unknown): string {
  if (typeof value === 'string') return JSON.stringify(value);
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  if (value === null) return 'nil';
  return JSON.stringify(value);
}

/** "Over limit, no manager" → OverLimitNoManager. */
function goIdentifier(name: string): string {
  const words = name.replace(/[^a-zA-Z0-9 ]/g, ' ').split(/\s+/).filter(Boolean);
  if (words.length === 0) return 'Scenario';
  return words.map((word) => word[0].toUpperCase() + word.slice(1)).join('');
}

export function curlSnippet(request: SimulationRequest, baseUrl: string): string {
  return `curl -X POST ${baseUrl}${SIMULATION_PATH} \\
  -H 'Content-Type: application/json' \\
  -H "Authorization: Bearer $METIS_TOKEN" \\
  -d '${JSON.stringify(request, null, 2)}'`;
}
