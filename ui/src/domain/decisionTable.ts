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
 * A new line's cells.
 *
 * Conditions start as the wildcard. They used to start as `""`, which is not
 * "anything" — it is "equals the empty string", so every line an author added
 * was one that could never match until they noticed and cleared it.
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
 */
export function parseOutputValue(raw: string, type: string): unknown {
  const text = raw.trim();
  if (text === '') return '';

  if (type === 'boolean') return text === 'true';

  if (type === 'number') {
    return NUMBER_PATTERN.test(text) ? Number(text) : text;
  }

  // Quotes are how the old editor wrote strings, and how anyone used to FEEL
  // will type one. They are punctuation here, not part of the value.
  if (text.length >= 2 && (text.startsWith('"') || text.startsWith("'")) && text.endsWith(text[0])) {
    return text.slice(1, -1);
  }
  if (text === 'true' || text === 'false') return text === 'true';
  if (NUMBER_PATTERN.test(text)) return Number(text);
  return text;
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

/**
 * Reads a condition cell back in words.
 *
 * The grid is compact because it has to be, and compact is exactly what makes
 * `]1..10]` unreadable to the person whose business rule it is. This is shown
 * on hover so nobody has to learn the notation to check the table is right.
 */
export function describeCell(cell: string, columnLabel: string): string {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return `${columnLabel}: any value`;

  const range = text.match(/^([[\]])\s*(-?[\d.]+)\s*\.\.\s*(-?[\d.]+)\s*([[\]])$/);
  if (range) {
    const [, open, low, high, close] = range;
    const from = open === '[' ? 'from' : 'above';
    const to = close === ']' ? 'up to' : 'below';
    return `${columnLabel} is ${from} ${low} ${to} ${high}`;
  }

  const comparison = text.match(/^(>=|<=|>|<|!=|=)\s*(.+)$/);
  if (comparison) {
    const [, operator, operand] = comparison;
    const words: Record<string, string> = {
      '>': 'is more than',
      '<': 'is less than',
      '>=': 'is at least',
      '<=': 'is at most',
      '!=': 'is not',
      '=': 'is',
    };
    return `${columnLabel} ${words[operator]} ${unquote(operand)}`;
  }

  if (text.startsWith('not(') && text.endsWith(')')) {
    return `${columnLabel} is not ${unquote(text.slice(4, -1))}`;
  }

  if (text.includes(',')) {
    const options = text.split(',').map((part) => unquote(part.trim()));
    return `${columnLabel} is ${options.slice(0, -1).join(', ')} or ${options[options.length - 1]}`;
  }

  return `${columnLabel} is ${unquote(text)}`;
}

function unquote(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length >= 2 && (trimmed.startsWith('"') || trimmed.startsWith("'")) && trimmed.endsWith(trimmed[0])) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

/** One thing wrong with the table, in the words of whoever has to fix it. */
export interface TableProblem {
  severity: 'error' | 'warning';
  message: string;
}

/**
 * What is wrong with the table, before it is saved.
 *
 * These are the failures that otherwise surface as an error from a running
 * process, hours later, attributed to the process rather than to the table.
 */
export function findProblems(
  hitPolicy: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
): TableProblem[] {
  const problems: TableProblem[] = [];
  const policy = hitPolicyOf(hitPolicy);

  if (policy?.needsValueList && !(outputs[0]?.values?.length)) {
    problems.push({
      severity: 'error',
      message: `“${policy.label}” ranks results by the list of allowed values on ${
        outputs[0]?.label || 'the first result column'
      }, and that list is empty. Add the values in order of importance, or choose another policy.`,
    });
  }

  outputs.forEach((output) => {
    if (!output.name.trim()) {
      problems.push({
        severity: 'error',
        message: `The result column “${output.label}” has no variable name, so nothing downstream can read it.`,
      });
    }
  });

  inputs.forEach((input) => {
    if (!input.expression.trim()) {
      problems.push({
        severity: 'error',
        message: `The condition column “${input.label}” does not say which value it tests.`,
      });
    }
  });

  if (rules.length === 0) {
    problems.push({ severity: 'warning', message: 'The table has no lines, so it will never decide anything.' });
  }

  rules.forEach((rule, index) => {
    const everythingIsWild = rule.input_entries.every((cell) => cell.trim() === '' || cell.trim() === ANY_VALUE);
    if (everythingIsWild && rules.length > 1 && index < rules.length - 1) {
      problems.push({
        severity: 'warning',
        message: `Line ${index + 1} matches everything, so no line below it can ever be reached${
          policy?.ordered ? '' : ' under this hit policy'
        }. Catch-all lines belong last.`,
      });
    }
    if (rule.output_entries.every((cell) => cell.trim() === '')) {
      problems.push({ severity: 'warning', message: `Line ${index + 1} has no result.` });
    }
  });

  // Two lines with identical conditions are a copy-paste, and under UNIQUE they
  // are a runtime failure rather than a smell.
  const seen = new Map<string, number>();
  rules.forEach((rule, index) => {
    const signature = rule.input_entries.map((cell) => cell.trim()).join(' ');
    const previous = seen.get(signature);
    if (previous !== undefined) {
      problems.push({
        severity: hitPolicy === 'UNIQUE' ? 'error' : 'warning',
        message: `Lines ${previous + 1} and ${index + 1} test exactly the same conditions.`,
      });
    } else {
      seen.set(signature, index);
    }
  });

  return problems;
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
  // than content. A range's `]…[` spellings mean depth can legitimately dip, so
  // only the final balance is checked.
  let depth = 0;
  let quote = '';
  for (const character of text) {
    if (quote) {
      if (character === quote) quote = '';
      continue;
    }
    if (character === '"' || character === "'") quote = character;
    else if (character === '(' || character === '[') depth += 1;
    else if (character === ')' || character === ']') depth -= 1;
  }
  if (depth !== 0) return 'A bracket is left open';

  if (/^(>=|<=|>|<|!=|=)\s*$/.test(text)) return 'This comparison has nothing to compare against';
  if (text.endsWith(',')) return 'This list ends with a comma';
  if (/\.\.\s*$/.test(text)) return 'This range has no upper end';

  return undefined;
}
