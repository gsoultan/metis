import { describe, expect, it } from 'bun:test';

import {
  ANY_VALUE,
  describeCell,
  describeTable,
  findProblems,
  formatOutputValue,
  moveRule,
  newRuleRow,
  parseOutputValue,
  parseClipboardGrid,
  applyPastedGrid,
  validateCell,
  type DecisionInputColumn,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from './decisionTable';

const inputs: DecisionInputColumn[] = [
  { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' },
];
const outputs: DecisionOutputColumn[] = [
  { id: 'o1', label: 'Severity', name: 'severity', type: 'string' },
];

function rule(conditions: string[], results: string[]): DecisionRuleRow {
  return { id: `${conditions.join()}-${results.join()}`, input_entries: conditions, output_entries: results };
}

/**
 * A result cell holds a value, not an expression — the engine puts it into a
 * process variable exactly as given. The editor used to run the cell text
 * through Number() and keep the string when that failed, which meant a table
 * saying "Approved" produced a value with the quote marks still in it, and an
 * empty cell produced the number zero.
 */
describe('parseOutputValue', () => {
  it('takes the quotes off a string, because they are punctuation not content', () => {
    expect(parseOutputValue('"Approved"', 'string')).toBe('Approved');
    expect(parseOutputValue("'Approved'", 'string')).toBe('Approved');
    expect(parseOutputValue('Approved', 'string')).toBe('Approved');
  });

  it('does not turn an empty cell into zero', () => {
    expect(parseOutputValue('', 'string')).toBe('');
    expect(parseOutputValue('   ', 'number')).toBe('');
  });

  it('reads numbers and booleans as themselves', () => {
    expect(parseOutputValue('42', 'number')).toBe(42);
    expect(parseOutputValue('-3.5', 'number')).toBe(-3.5);
    expect(parseOutputValue('true', 'boolean')).toBe(true);
    expect(parseOutputValue('false', 'boolean')).toBe(false);
  });

  it('keeps text that only looks numeric in a text column as text', () => {
    expect(parseOutputValue('"007"', 'string')).toBe('007');
  });

  it('round-trips through formatOutputValue', () => {
    expect(formatOutputValue(parseOutputValue('"Approved"', 'string'))).toBe('Approved');
    expect(formatOutputValue(parseOutputValue('42', 'number'))).toBe('42');
  });

  it('unquotes a value stored by the old editor when loading it', () => {
    expect(formatOutputValue('"ok"')).toBe('ok');
  });
});

/**
 * The wildcard. A new line used to arrive with `""` in every condition, which
 * is not "anything" — it is "equals the empty string" — so every line an author
 * added was one that could never match.
 */
describe('newRuleRow', () => {
  it('starts conditions at the wildcard, not at the empty string', () => {
    const row = newRuleRow('r1', 2, 1);
    expect(row.input_entries).toEqual([ANY_VALUE, ANY_VALUE]);
    expect(row.output_entries).toEqual(['']);
  });
});

describe('describeCell', () => {
  it('reads the notation back in words', () => {
    expect(describeCell('> 10', 'Amount')).toBe('Amount is more than 10');
    expect(describeCell('>= 10', 'Amount')).toBe('Amount is at least 10');
    expect(describeCell('[1..10]', 'Amount')).toBe('Amount is from 1 up to 10');
    expect(describeCell(']1..10]', 'Amount')).toBe('Amount is above 1 up to 10');
    expect(describeCell('not("VIP")', 'Tier')).toBe('Tier is not VIP');
    expect(describeCell('"A", "B"', 'Tier')).toBe('Tier is A or B');
    expect(describeCell('"GOLD"', 'Tier')).toBe('Tier is GOLD');
  });

  it('says what an empty cell means, which is the thing nobody guesses right', () => {
    expect(describeCell('', 'Amount')).toBe('Amount: any value');
    expect(describeCell('-', 'Amount')).toBe('Amount: any value');
  });
});

/**
 * PRIORITY and OUTPUT ORDER rank by the result column's list of allowed values
 * and refuse to run without it. Catching that here is the difference between a
 * message in the editor and a failed process instance.
 */
describe('findProblems', () => {
  it('refuses a ranking policy with nothing to rank by', () => {
    const problems = findProblems('PRIORITY', inputs, outputs, [rule(['> 10'], ['HIGH'])]);
    expect(problems.some((p) => p.severity === 'error' && p.message.includes('list of allowed values'))).toBe(true);
  });

  it('accepts it once the list is there', () => {
    const ranked: DecisionOutputColumn[] = [{ ...outputs[0], values: ['HIGH', 'LOW'] }];
    const problems = findProblems('PRIORITY', inputs, ranked, [rule(['> 10'], ['HIGH'])]);
    expect(problems.filter((p) => p.severity === 'error')).toEqual([]);
  });

  it('points out a catch-all line that hides everything below it', () => {
    const problems = findProblems('FIRST', inputs, outputs, [
      rule([ANY_VALUE], ['LOW']),
      rule(['> 10'], ['HIGH']),
    ]);
    expect(problems.some((p) => p.message.includes('matches everything'))).toBe(true);
  });

  it('treats two identical lines as fatal under UNIQUE and a smell otherwise', () => {
    const duplicated = [rule(['> 10'], ['HIGH']), rule(['> 10'], ['LOW'])];
    expect(findProblems('UNIQUE', inputs, outputs, duplicated).some((p) => p.severity === 'error')).toBe(true);
    expect(findProblems('FIRST', inputs, outputs, duplicated).some((p) => p.severity === 'warning')).toBe(true);
  });

  it('names a result column that nothing downstream can read', () => {
    const nameless: DecisionOutputColumn[] = [{ ...outputs[0], name: '' }];
    expect(findProblems('FIRST', inputs, nameless, [rule(['> 10'], ['HIGH'])]).some((p) => p.severity === 'error')).toBe(
      true,
    );
  });
});

describe('describeTable', () => {
  it('says what the table does in one sentence', () => {
    expect(describeTable('FIRST', '', inputs, outputs, 2)).toBe(
      'Looks at Amount, decides Severity. 2 lines; when several match, the first line that matches wins.',
    );
  });

  it('folds the aggregation into the sentence', () => {
    expect(describeTable('COLLECT', 'SUM', inputs, outputs, 1)).toContain('collects every match and add them up');
  });
});

describe('moveRule', () => {
  const rows = [rule(['a'], ['1']), rule(['b'], ['2']), rule(['c'], ['3'])];

  it('moves a line and leaves the rest in order', () => {
    expect(moveRule(rows, 2, 0).map((r) => r.input_entries[0])).toEqual(['c', 'a', 'b']);
  });

  it('does nothing at the edges', () => {
    expect(moveRule(rows, 0, -1)).toBe(rows);
    expect(moveRule(rows, 2, 3)).toBe(rows);
  });
});

/**
 * Decision tables are built in a spreadsheet first — that is where the rates and
 * the bands already live. Accepting what a spreadsheet puts on the clipboard is
 * the difference between an afternoon of retyping and a minute.
 */
describe('parseClipboardGrid', () => {
  it('reads tab-separated lines', () => {
    expect(parseClipboardGrid('> 10\tGOLD\n> 20\tSILVER')).toEqual([
      ['> 10', 'GOLD'],
      ['> 20', 'SILVER'],
    ]);
  });

  it('survives Windows line endings and a trailing newline', () => {
    expect(parseClipboardGrid('a\tb\r\nc\td\r\n')).toEqual([
      ['a', 'b'],
      ['c', 'd'],
    ]);
  });

  it('treats a single value as a one-cell grid', () => {
    expect(parseClipboardGrid('GOLD')).toEqual([['GOLD']]);
  });

  it('reads nothing from nothing', () => {
    expect(parseClipboardGrid('')).toEqual([]);
  });
});

describe('applyPastedGrid', () => {
  const start = [newRuleRow('r1', 2, 1)];
  const id = (() => {
    let n = 0;
    return () => `new-${n++}`;
  })();

  it('fills across conditions and then results', () => {
    const [row] = applyPastedGrid(start, 0, 0, [['> 10', 'GOLD', 'BULK']], 2, 1, id);
    expect(row.input_entries).toEqual(['> 10', 'GOLD']);
    expect(row.output_entries).toEqual(['BULK']);
  });

  it('adds the lines the paste needs', () => {
    const rows = applyPastedGrid(start, 0, 0, [['a', 'b', 'c'], ['d', 'e', 'f']], 2, 1, id);
    expect(rows).toHaveLength(2);
    expect(rows[1].input_entries).toEqual(['d', 'e']);
  });

  it('drops columns past the end rather than inventing variables for them', () => {
    const [row] = applyPastedGrid(start, 0, 0, [['a', 'b', 'c', 'ignored']], 2, 1, id);
    expect(row.input_entries).toEqual(['a', 'b']);
    expect(row.output_entries).toEqual(['c']);
  });

  it('starts where the cursor is, not at the top left', () => {
    const [row] = applyPastedGrid(start, 0, 1, [['GOLD', 'BULK']], 2, 1, id);
    expect(row.input_entries[0]).toBe(ANY_VALUE);
    expect(row.input_entries[1]).toBe('GOLD');
    expect(row.output_entries[0]).toBe('BULK');
  });

  it('leaves the original rows alone', () => {
    applyPastedGrid(start, 0, 0, [['x', 'y', 'z']], 2, 1, id);
    expect(start[0].input_entries).toEqual([ANY_VALUE, ANY_VALUE]);
  });
});

describe('validateCell', () => {
  it('accepts the notations a table is written in', () => {
    for (const cell of ['', '-', '> 10', '[1..10]', ']1..10[', '"A", "B"', 'not("A")', 'GOLD']) {
      expect(validateCell(cell)).toBeUndefined();
    }
  });

  it('catches what is wrong however you read it', () => {
    expect(validateCell('"GOLD')).toBe('A quote is left open');
    expect(validateCell('not("A"')).toBe('A bracket is left open');
    expect(validateCell('>')).toBe('This comparison has nothing to compare against');
    expect(validateCell('10, 20,')).toBe('This list ends with a comma');
    expect(validateCell('[1..')).toBe('A bracket is left open');
    expect(validateCell('1..')).toBe('This range has no upper end');
  });

  it('does not count brackets inside quoted text', () => {
    expect(validateCell('"a [ b"')).toBeUndefined();
  });
});
