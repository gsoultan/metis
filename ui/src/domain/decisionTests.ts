/**
 * The examples a decision table is expected to get right, as the editor holds
 * them and as the engine receives them.
 *
 * The editor holds every cell as text, because that is what a person types into
 * a grid. The engine wants values. The translation is the same one a result cell
 * gets — an author writing `Approved` means the word and `500` means the number
 * — and it lives here rather than in the component so it can be checked.
 */
import { parseOutputValue, type DecisionInputColumn, type DecisionOutputColumn } from './decisionTable';

/** One example, as the editor holds it: plain text per column. */
export interface DecisionTestRow {
  id: string;
  name: string;
  /** Keyed by input expression. */
  inputs: Record<string, string>;
  /** Keyed by output name. */
  expected: Record<string, string>;
}

/** One example, as the API carries it. */
export interface DecisionTestPayload {
  id: string;
  name: string;
  inputs: Record<string, unknown>;
  expected: Record<string, unknown>;
}

/**
 * Turns the editor's text into the values the engine will see.
 *
 * A blank cell is omitted rather than sent as an empty string. On the input side
 * that is the difference between "this variable is absent" and "this variable is
 * the empty string"; on the expectation side it is the difference between
 * checking an output and not mentioning it — which is what lets a table grow a
 * column without invalidating every example written before it.
 */
export function testsToPayload(
  tests: DecisionTestRow[],
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
): DecisionTestPayload[] {
  return tests.map((test) => ({
    id: test.id,
    name: test.name,
    inputs: Object.fromEntries(
      inputs
        .filter((input) => (test.inputs[input.expression] ?? '').trim() !== '')
        .map((input) => [input.expression, parseOutputValue(test.inputs[input.expression], input.type)]),
    ),
    expected: Object.fromEntries(
      outputs
        .filter((output) => (test.expected[output.name] ?? '').trim() !== '')
        .map((output) => [output.name, parseOutputValue(test.expected[output.name], output.type)]),
    ),
  }));
}
