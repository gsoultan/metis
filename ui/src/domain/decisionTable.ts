/**
 * What a decision table means, separated from how it is drawn.
 *
 * A decision table is a business document that a non-programmer is expected to
 * read and change. Most of what makes one confusing is not the grid — it is the
 * things the grid does not say: which line wins when two apply, whether the
 * order of the lines matters, and whether an empty cell means "anything" or
 * "empty". This module answers those in words, so the editor can print the
 * answer next to the table instead of hiding it in a dropdown.
 */

/** A condition column: the value being tested. */
export interface DecisionInputColumn {
  id: string;
  label: string;
  expression: string;
  /** "string" | "number" | "boolean" | "date" */
  type: string;
}

/** A result column: the value the table produces. */
export interface DecisionOutputColumn {
  id: string;
  label: string;
  name: string;
  type: string;
  /**
   * The allowed results, most important first. PRIORITY and OUTPUT ORDER rank
   * matches by this list and refuse to run without it.
   */
  values?: string[];
}

/** One line of the table. */
export interface DecisionRuleRow {
  id: string;
  input_entries: string[];
  output_entries: string[];
  description?: string;
}

/**
 * A hit policy, in terms of the question it settles.
 *
 * The DMN letter codes are kept alongside because they are what appears in a
 * table exported to or imported from another engine — but nobody building a
 * pricing table thinks in letters, they think "what if two lines both apply?".
 */
export interface HitPolicy {
  value: string;
  /** The answer to "when several lines match, what happens?" */
  label: string;
  description: string;
  /** The DMN spelling, for import and export. */
  dmn: string;
  /** True when the answer is a list rather than a single value. */
  multi: boolean;
  /** True when the table's own top-to-bottom order changes the answer. */
  ordered: boolean;
  /** True when it ranks by the result column's list of allowed values. */
  needsValueList: boolean;
}

export const HIT_POLICIES: HitPolicy[] = [
  {
    value: 'FIRST',
    label: 'The first line that matches wins',
    description:
      'Lines are checked top to bottom and the first match is the answer. Put the most specific lines first.',
    dmn: 'FIRST (F)',
    multi: false,
    ordered: true,
    needsValueList: false,
  },
  {
    value: 'UNIQUE',
    label: 'Only one line may match',
    description:
      'If two lines match at once the decision fails. Use it when overlapping lines would be a mistake worth catching early.',
    dmn: 'UNIQUE (U)',
    multi: false,
    ordered: false,
    needsValueList: false,
  },
  {
    value: 'ANY',
    label: 'Several may match, but they must agree',
    description:
      'More than one line may match, as long as they all give the same result. If they disagree the decision fails, because the table contradicts itself.',
    dmn: 'ANY (A)',
    multi: false,
    ordered: false,
    needsValueList: false,
  },
  {
    value: 'PRIORITY',
    label: 'The most important result wins',
    description:
      'Every matching line is considered and the one whose result ranks highest is the answer. Ranking comes from the result column’s list of allowed values.',
    dmn: 'PRIORITY (P)',
    multi: false,
    ordered: false,
    needsValueList: true,
  },
  {
    value: 'COLLECT',
    label: 'Collect every match',
    description:
      'The answer is a list holding the result of every line that matched. Add a summary — a sum or a count — to reduce that list to one number.',
    dmn: 'COLLECT (C)',
    multi: true,
    ordered: false,
    needsValueList: false,
  },
  {
    value: 'OUTPUT ORDER',
    label: 'Every match, most important first',
    description:
      'The answer is a list of every match, sorted by the result column’s list of allowed values rather than by where the lines sit in the table.',
    dmn: 'OUTPUT ORDER (O)',
    multi: true,
    ordered: false,
    needsValueList: true,
  },
  {
    value: 'RULE ORDER',
    label: 'Every match, in the order written',
    description:
      'The answer is a list of every match, in the order the lines appear in the table.',
    dmn: 'RULE ORDER (R)',
    multi: true,
    ordered: true,
    needsValueList: false,
  },
];

export function hitPolicyOf(value: string): HitPolicy | undefined {
  return HIT_POLICIES.find((policy) => policy.value === value);
}

/** The aggregations COLLECT can apply to the list it gathers. */
export const AGGREGATIONS = [
  { value: '', label: 'Keep the whole list' },
  { value: 'SUM', label: 'Add them up' },
  { value: 'COUNT', label: 'Count them' },
  { value: 'MIN', label: 'Take the smallest' },
  { value: 'MAX', label: 'Take the largest' },
];

/** The wildcard cell: this column does not matter for this line. */
export const ANY_VALUE = '-';

/**
 * Ready-made conditions, per column type.
 *
 * Someone writing their first table does not know that `]1..10]` excludes the
 * lower bound, and should not have to. Picking the sentence writes the notation.
 */
export const CELL_TEMPLATES: Record<string, { value: string; label: string }[]> = {
  string: [
    { value: ANY_VALUE, label: 'Any value' },
    { value: 'Approved', label: 'Exactly this word' },
    { value: '"A", "B"', label: 'Either of two values' },
    { value: 'not("A")', label: 'Anything except' },
    { value: '""', label: 'Empty' },
  ],
  number: [
    { value: ANY_VALUE, label: 'Any number' },
    { value: '> 10', label: 'More than' },
    { value: '>= 10', label: 'At least' },
    { value: '< 10', label: 'Less than' },
    { value: '[1..10]', label: 'Between, inclusive' },
    { value: ']1..10]', label: 'Between, excluding the low end' },
    { value: '10, 20', label: 'One of several' },
  ],
  boolean: [
    { value: ANY_VALUE, label: 'Either' },
    { value: 'true', label: 'Yes' },
    { value: 'false', label: 'No' },
  ],
  date: [
    { value: ANY_VALUE, label: 'Any date' },
    { value: '> "2024-01-01"', label: 'After' },
    { value: '< "2024-01-01"', label: 'Before' },
  ],
};

export const COLUMN_TYPES = [
  { value: 'string', label: 'Text' },
  { value: 'number', label: 'Number' },
  { value: 'boolean', label: 'Yes / no' },
  { value: 'date', label: 'Date' },
];

/**
 * The process variable a column reads or writes, derived from its heading.
 *
 * "Approval level" becomes `approval_level`, the same way the creation wizard
 * turns a decision's name into its key. Nobody authoring a table should have to
 * know that a variable name cannot contain a space.
 */
export function slugVariable(label: string): string {
  return label
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
}

/**
 * Whether a variable is still following its heading, and so should keep
 * following it when the heading changes. Once somebody has typed their own
 * name, a later edit to the heading leaves it alone.
 */
export function variableFollowsLabel(variable: string, label: string): boolean {
  return variable.trim() === '' || variable === slugVariable(label);
}

/**
 * A new line's cells.
 *
 * An empty condition cell and `-` both mean "any value" to the engine. New
 * lines are written with the wildcard rather than left blank so the meaning is
 * visible in the grid; the only way to say "equals the empty string" is the
 * quoted `""`, which the cell menu offers as "Empty".
 */
export function newRuleRow(id: string, inputCount: number, outputCount: number): DecisionRuleRow {
  return {
    id,
    input_entries: Array<string>(inputCount).fill(ANY_VALUE),
    output_entries: Array<string>(outputCount).fill(''),
    description: '',
  };
}

const NUMBER_PATTERN = /^-?\d+(\.\d+)?$/;

/**
 * Turns what an author typed in a result cell into the value the process will
 * receive.
 *
 * A result is a plain value, not an expression: the engine stores it in a
 * process variable exactly as given. The editor used to send the cell text
 * through `Number()` and keep the string when that failed, so a cell holding
 * `"Approved"` produced the six characters `Approved` *with the quotes*, and an
 * empty cell produced the number zero.
 *
 * The declared column type decides the value's type. A text column holding
 * `10` produces the text "10", not the number ten: the author chose "Text" and
 * a downstream condition comparing it to a string must keep working.
 */
export function parseOutputValue(raw: string, type: string): string | number | boolean {
  const text = raw.trim();
  if (text === '') return '';

  if (type === 'boolean') return text === 'true';

  if (type === 'number') {
    return NUMBER_PATTERN.test(text) ? Number(text) : text;
  }

  // Quotes are how the old editor wrote strings, and how anyone used to FEEL
  // will type one. They are punctuation here, not part of the value.
  return unquote(text);
}

/** Renders a stored result for editing, undoing parseOutputValue's quoting. */
export function formatOutputValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  const text = String(value);
  if (
    typeof value === 'string' &&
    text.length >= 2 &&
    (text.startsWith('"') || text.startsWith("'")) &&
    text.endsWith(text[0])
  ) {
    return text.slice(1, -1);
  }
  return text;
}

/** The headings of a table's condition columns, by the variable each reads. */
export type ColumnHeadings = ReadonlyMap<string, string>;

const NO_HEADINGS: ColumnHeadings = new Map();

export function columnHeadingsOf(inputs: DecisionInputColumn[]): ColumnHeadings {
  return new Map(
    inputs
      .filter((input) => input.expression.trim() !== '')
      .map((input) => [input.expression.trim(), input.label || input.expression.trim()]),
  );
}

const COMPARISON_WORDS: Record<string, string> = {
  '>': 'is more than',
  '<': 'is less than',
  '>=': 'is at least',
  '<=': 'is at most',
  '!=': 'is not',
  '=': 'is',
};

/**
 * Reads a condition cell back in words.
 *
 * The grid is compact because it has to be, and compact is exactly what makes
 * `]1..10]` unreadable to the person whose business rule it is. This is shown
 * on hover so nobody has to learn the notation to check the table is right.
 *
 * A name that is another column's is read as that column: `> minimum` is "more
 * than Minimum", and `minimum` alone "the same as Minimum". The engine compares
 * with that column's value, not with the word.
 */
export function describeCell(cell: string, columnLabel: string, headings: ColumnHeadings = NO_HEADINGS): string {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return `${columnLabel}: any value`;
  if (text === '""' || text === "''") return `${columnLabel} is empty`;
  const value = (operand: string) => headings.get(operand.trim()) ?? unquote(operand);

  const range = text.match(/^([[\]])\s*(-?[\d.]+|[A-Za-z_]\w*)\s*\.\.\s*(-?[\d.]+|[A-Za-z_]\w*)\s*([[\]])$/);
  if (range) {
    const [, open, low, high, close] = range;
    const from = open === '[' ? 'from' : 'above';
    const to = close === ']' ? 'up to' : 'below';
    return `${columnLabel} is ${from} ${value(low)} ${to} ${value(high)}`;
  }

  const comparison = text.match(/^(>=|<=|>|<|!=|=)\s*(.+)$/);
  if (comparison) {
    const [, operator, operand] = comparison;
    return `${columnLabel} ${COMPARISON_WORDS[operator]} ${value(operand)}`;
  }

  if (text.startsWith('not(') && text.endsWith(')')) {
    return `${columnLabel} is not ${value(text.slice(4, -1))}`;
  }

  if (text.includes(',')) {
    const options = text.split(',').map((part) => value(part.trim()));
    return `${columnLabel} is ${options.slice(0, -1).join(', ')} or ${options[options.length - 1]}`;
  }

  const other = headings.get(text);
  return other ? `${columnLabel} is the same as ${other}` : `${columnLabel} is ${unquote(text)}`;
}

/** The hint under the grid: a condition can compare with another column. */
export interface ColumnComparisonHint {
  /** A condition naming another column, as it is typed. */
  example: string;
  /** What it means, when the table has another column to name. */
  meaning?: string;
}

/**
 * The hint under the grid that a condition can name another column.
 *
 * The engine tests a condition with the rest of the case in scope, so
 * `> minimum` compares a score with the minimum beside it — which nobody
 * guesses from a grid of literals. The example names a column the table has,
 * once it has two.
 */
export function columnComparisonHint(inputs: DecisionInputColumn[]): ColumnComparisonHint {
  const others = inputs.slice(1).filter((input) => input.expression.trim() !== '');
  const other = others[others.length - 1];
  if (!other) return { example: '> minimum' };
  const name = other.expression.trim();
  const heading = other.label || name;
  return other.type === 'number' || other.type === 'date'
    ? { example: `> ${name}`, meaning: `more than ${heading}` }
    : { example: `= ${name}`, meaning: `the same as ${heading}` };
}

/**
 * The shape of a condition cell with its incidental differences removed, so
 * that two lines saying the same thing in different spelling are seen as the
 * same: `"A"` and `A`, `> 10` and `>10`, a blank cell and the wildcard.
 */
export function normalizeCell(cell: string): string {
  const text = cell.trim().replace(/\s+/g, ' ');
  if (text === '' || text === ANY_VALUE) return ANY_VALUE;
  return unquote(text).replace(/\s*([<>=!.,]+)\s*/g, '$1');
}

function unquote(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length >= 2 && (trimmed.startsWith('"') || trimmed.startsWith("'")) && trimmed.endsWith(trimmed[0])) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

/** The table, in one sentence, for the person who has to trust it. */
export function describeTable(
  hitPolicy: string,
  aggregation: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  ruleCount: number,
): string {
  const policy = hitPolicyOf(hitPolicy);
  const tested = inputs.map((input) => input.label || input.expression).filter(Boolean);
  const produced = outputs.map((output) => output.label || output.name).filter(Boolean);

  const looks = tested.length ? `Looks at ${joinWords(tested)}` : 'Looks at nothing yet';
  const gives = produced.length ? `decides ${joinWords(produced)}` : 'decides nothing yet';
  const lines = `${ruleCount} line${ruleCount === 1 ? '' : 's'}`;

  let settles = policy ? policy.label.toLowerCase() : hitPolicy;
  if (hitPolicy === 'COLLECT' && aggregation) {
    const summary = AGGREGATIONS.find((entry) => entry.value === aggregation);
    settles = `collects every match and ${(summary?.label ?? aggregation).toLowerCase()}`;
  }

  return `${looks}, ${gives}. ${lines}; when several match, ${settles}.`;
}

function joinWords(words: string[]): string {
  if (words.length === 1) return words[0];
  return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
}

/** Moves a line, for the policies where the table's order is the meaning. */
export function moveRule(rules: DecisionRuleRow[], from: number, to: number): DecisionRuleRow[] {
  if (to < 0 || to >= rules.length || from === to) return rules;
  const next = [...rules];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/**
 * Splits what came off the clipboard into a grid.
 *
 * People build decision tables in a spreadsheet first — that is where the rates
 * and the bands already are — and then retype them. Tab-separated with newlines
 * between lines is what every spreadsheet puts on the clipboard, so accepting it
 * is the difference between an afternoon and a minute.
 */
export function parseClipboardGrid(text: string): string[][] {
  const normalised = text.replace(/\r\n?/g, '\n').replace(/\n$/, '');
  if (normalised === '') return [];
  return normalised.split('\n').map((line) => line.split('\t'));
}

/**
 * Writes a pasted grid into the table, starting at one cell.
 *
 * Lines beyond the end are added; columns beyond the end are dropped, because
 * adding a column means naming a variable and choosing a type, which a paste
 * cannot decide. Cells are laid out left to right across the conditions and then
 * the results, which is the order they are read in.
 */
export function applyPastedGrid(
  rules: DecisionRuleRow[],
  atRow: number,
  atColumn: number,
  grid: string[][],
  inputCount: number,
  outputCount: number,
  makeId: () => string,
): DecisionRuleRow[] {
  if (grid.length === 0) return rules;

  const width = inputCount + outputCount;
  const next = rules.map((rule) => ({
    ...rule,
    input_entries: [...rule.input_entries],
    output_entries: [...rule.output_entries],
  }));

  grid.forEach((line, lineOffset) => {
    const rowIndex = atRow + lineOffset;
    while (next.length <= rowIndex) {
      next.push(newRuleRow(makeId(), inputCount, outputCount));
    }
    const row = next[rowIndex];

    line.forEach((cell, cellOffset) => {
      const column = atColumn + cellOffset;
      if (column >= width) return;
      if (column < inputCount) {
        row.input_entries[column] = cell;
      } else {
        row.output_entries[column - inputCount] = cell;
      }
    });
  });

  return next;
}

/**
 * What is wrong with one condition cell, if anything.
 *
 * Deliberately shallow: the real grammar lives in the Go parser and cannot run
 * in a browser, so this catches the shapes that are wrong however you read them
 * — an unclosed quote, an unbalanced bracket, an operator with nothing after it.
 * Anything subtler is caught when the table is tried, which is one click away.
 */
export function validateCell(cell: string): string | undefined {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return undefined;

  const doubleQuotes = (text.match(/"/g) ?? []).length;
  const singleQuotes = (text.match(/'/g) ?? []).length;
  if (doubleQuotes % 2 !== 0 || singleQuotes % 2 !== 0) return 'A quote is left open';

  // Brackets are counted outside quoted text, where they are punctuation rather
  // than content. A range may open with `]` or close with `[` — that is how DMN
  // spells an excluded end, and the cell menu offers it — so at the two ends of
  // the cell a reversed bracket is a delimiter, not an unmatched one.
  let depth = 0;
  let quote = '';
  let questionMark = false;
  for (const character of asRangeDelimited(text)) {
    if (quote) {
      if (character === quote) quote = '';
      continue;
    }
    if (character === '"' || character === "'") quote = character;
    else if (character === '(' || character === '[') depth += 1;
    else if (character === ')' || character === ']') depth -= 1;
    else if (character === '?') questionMark = true;
  }
  if (depth !== 0) return 'A bracket is left open';
  // `?` is how Camunda writes a condition's own value. The engine's FEEL has no
  // `?`, so the table would fail when it runs; a condition already tests its
  // own column, which is all `? > minimum` asks.
  if (questionMark) return 'Leave out the ?: a condition already tests its own column, as in > minimum';

  if (/^(>=|<=|>|<|!=|=)\s*$/.test(text)) return 'This comparison has nothing to compare against';
  if (text.endsWith(',')) return 'This list ends with a comma';
  if (/\.\.\s*$/.test(text)) return 'This range has no upper end';

  return undefined;
}

/** `]1..10[` read as `[1..10]`, so the bracket count sees a closed range. */
function asRangeDelimited(text: string): string {
  let result = text;
  if (result.startsWith(']')) result = `[${result.slice(1)}`;
  if (result.endsWith('[') && result.includes('..')) result = `${result.slice(0, -1)}]`;
  return result;
}
