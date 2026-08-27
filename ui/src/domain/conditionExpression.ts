/**
 * Builds and reads the comparison a non-technical user edits as three fields:
 * variable · operator · value.
 *
 * What it emits is FEEL, the engine's condition language. The builder used to
 * emit `js:`-prefixed JavaScript, which the server now refuses by default —
 * the runtime behind it cannot be memory-bounded, so a condition authored
 * that way is a decision point that refuses to route. Nothing the three
 * fields can express needs JavaScript.
 *
 * Reading is more tolerant than writing: stored conditions include legacy
 * shapes (`js:amount > 100`, bare `status=approved`), and the builder must
 * open them for editing rather than showing empty fields over a condition
 * that plainly exists.
 */

export type ComparisonOperator = '==' | '!=' | '>' | '<' | '>=' | '<=';

export interface Comparison {
  variable: string;
  operator: ComparisonOperator;
  /** As the user typed it; quoting is decided when the condition is built. */
  value: string;
}

/** FEEL spells equality `=`; the UI keeps `==` because that is what the
 * operator dropdown has always said and stored values may echo back. */
const OPERATOR_TO_FEEL: Record<ComparisonOperator, string> = {
  '==': '=',
  '!=': '!=',
  '>': '>',
  '<': '<',
  '>=': '>=',
  '<=': '<=',
};

/**
 * A value becomes a FEEL literal: numbers and booleans stay bare, anything
 * else is double-quoted. Without the quoting, `status != approved` would
 * compare two variables — and `approved` the variable is almost never what
 * someone typing `approved` the word meant.
 */
function toLiteral(value: string): string {
  const trimmed = value.trim();
  if (trimmed === 'true' || trimmed === 'false' || trimmed === 'null') {
    return trimmed;
  }
  if (trimmed !== '' && !Number.isNaN(Number(trimmed))) {
    return trimmed;
  }
  return `"${stripQuotes(trimmed).replaceAll('"', '\\"')}"`;
}

/** Users following the old placeholder text quote strings themselves
 * (`'approved'`); one layer of matching quotes is theirs, not the value's. */
function stripQuotes(value: string): string {
  if (value.length >= 2) {
    const first = value[0];
    if ((first === '"' || first === "'") && value.endsWith(first)) {
      return value.slice(1, -1);
    }
  }
  return value;
}

/**
 * Builds the stored condition. Both fields are required to say anything: a
 * half-typed comparison stores as empty rather than as text no evaluator
 * accepts — and empty means "no condition", which is at least a defined
 * answer while the user is still typing.
 */
export function buildCondition(comparison: Comparison): string {
  const variable = comparison.variable.trim();
  const value = comparison.value.trim();
  if (variable === '' || value === '') {
    return '';
  }
  return `${variable} ${OPERATOR_TO_FEEL[comparison.operator]} ${toLiteral(value)}`;
}

const COMPARISON_SHAPE = /^([A-Za-z_][\w.]*)\s*(==|!=|>=|<=|=|>|<)\s*(.+)$/;

/**
 * True when the text after the first operator carries more expression —
 * another comparison, or a boolean keyword outside quotes. `100 and x = 2`
 * is compound; `"black and white"` is a value that happens to contain a word.
 */
function isCompound(rest: string): boolean {
  const withoutStrings = rest.replace(/"(?:\\.|[^"\\])*"|'[^']*'/g, '""');
  return /[<>!=]/.test(withoutStrings) || /\b(and|or|not|in|between)\b/i.test(withoutStrings);
}

/**
 * Reads a stored condition back into the three fields, accepting every shape
 * the builder has ever written. Returns null for anything richer — a
 * composite FEEL expression is not three fields, and pretending it parsed
 * would let an edit silently overwrite the parts that did not fit.
 */
export function parseCondition(condition: string): Comparison | null {
  const raw = condition.startsWith('js:') ? condition.slice(3) : condition;
  const match = COMPARISON_SHAPE.exec(raw.trim());
  if (!match) {
    return null;
  }
  const [, variable, operator, rest] = match;
  if (isCompound(rest)) {
    return null;
  }
  return {
    variable,
    operator: operator === '=' ? '==' : (operator as ComparisonOperator),
    value: stripQuotes(rest.trim()).replaceAll('\\"', '"'),
  };
}

/**
 * True when a stored condition exists but is more than the three-field builder
 * can represent — a compound expression, or JavaScript doing real work.
 *
 * The builder must not present empty fields over one of these: the first
 * keystroke would call buildCondition and replace an expression the user never
 * chose to discard. Callers show the expression and ask instead.
 */
export function isTooRichForBuilder(condition: string): boolean {
  return condition.trim() !== '' && parseCondition(condition) === null;
}
