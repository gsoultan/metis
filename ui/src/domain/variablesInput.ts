/**
 * Turning a box of typed JSON into process variables.
 *
 * Variables are the business payload — an amount, an order number, an approval
 * — so the person typing them is usually testing a hypothesis about a process,
 * not writing JSON. The messages here are written for that person: "Line 3: a
 * name needs double quotes" rather than the engine's own
 * `Unexpected token } in JSON at position 41`.
 *
 * Empty means empty. A blank box is "send no variables", not an error, because
 * most processes start with none.
 */
import type { ProcessVariables } from '../services/types';

export type VariablesInput =
  | { ok: true; value: ProcessVariables }
  | { ok: false; message: string };

export function parseVariables(text: string): VariablesInput {
  const trimmed = text.trim();
  if (trimmed === '') {
    return { ok: true, value: {} };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (error) {
    const raw = error instanceof Error ? error.message : String(error);
    return { ok: false, message: explainJsonError(raw, trimmed) };
  }

  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return {
      ok: false,
      message: 'Variables are a set of names and values, so this needs to be an object: { "amount": 42 }',
    };
  }

  return { ok: true, value: parsed as ProcessVariables };
}

/**
 * The parser's complaint, rewritten with the line it is about.
 *
 * Exported because the shape of that complaint is the engine's choice, not
 * ours: V8 says `… in JSON at position 41 (line 3 column 3)`, JavaScriptCore
 * says `JSON Parse error: Expected '}'` and names nothing at all. Taking the
 * raw message as an argument is what lets both be covered by a test that does
 * not depend on which engine ran it.
 *
 * A character offset is unusable in a textarea — nobody counts to 41 — so it is
 * turned into a line number. The two usual causes are named outright, because
 * a single quote and a trailing comma are most of what goes wrong here and
 * neither engine says so.
 */
export function explainJsonError(rawMessage: string, text: string): string {
  const where = locate(rawMessage, text);

  if (text.includes("'")) {
    return `${where}JSON needs double quotes — "name", not 'name'.`;
  }
  if (/,\s*[}\]]/.test(text)) {
    return `${where}There is a comma after the last entry. JSON does not allow one.`;
  }
  return `${where}This is not valid JSON yet.`;
}

/** `Line 3: `, or nothing when the engine did not say where. */
function locate(rawMessage: string, text: string): string {
  const named = /line (\d+)/i.exec(rawMessage)?.[1];
  if (named !== undefined) {
    return `Line ${named}: `;
  }
  const offset = Number(/position (\d+)/.exec(rawMessage)?.[1] ?? NaN);
  if (!Number.isFinite(offset)) {
    return '';
  }
  return `Line ${text.slice(0, offset).split('\n').length}: `;
}

/** Pretty-prints whatever is in the box, leaving invalid JSON untouched. */
export function formatVariables(text: string): string {
  const parsed = parseVariables(text);
  if (!parsed.ok) {
    return text;
  }
  return Object.keys(parsed.value).length === 0 ? '' : JSON.stringify(parsed.value, null, 2);
}
