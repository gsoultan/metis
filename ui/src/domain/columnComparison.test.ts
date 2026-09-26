/**
 * A condition that names another column, as the editor's checks read it.
 *
 * The engine tests a condition against its own column with the rest of the
 * case in scope, so `> minimum` compares a score with the minimum beside it and
 * `minimum` on its own means that column's value, not the word. Each check the
 * editor runs — the cell's own underline, the problems that stop a save, and
 * the coverage card — has to read the table that way too, or it flags a table
 * that is right, or passes one it has misread.
 */
import { describe, expect, it } from 'bun:test';

import { findCoverageGaps, whyNotChecked } from './decisionCoverage';
import { findProblems } from './decisionProblems';
import {
  describeCell,
  validateCell,
  type DecisionInputColumn,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from './decisionTable';

const score: DecisionInputColumn = { id: 'i1', label: 'Score', expression: 'score', type: 'number' };
const minimum: DecisionInputColumn = { id: 'i2', label: 'Minimum', expression: 'minimum', type: 'number' };
const verdict: DecisionOutputColumn = { id: 'o1', label: 'Verdict', name: 'verdict', type: 'string' };
const headings = new Map([
  ['score', 'Score'],
  ['minimum', 'Minimum'],
]);

function line(id: string, conditions: string[], result: string): DecisionRuleRow {
  return { id, input_entries: conditions, output_entries: [result] };
}

/** Pass a score above the minimum beside it; fail the rest. */
const passMark = [line('pass', ['> minimum', '-'], 'PASS'), line('fail', ['-', '-'], 'FAIL')];

describe('a condition that names another column', () => {
  it('is not marked as a mistake in its cell', () => {
    for (const cell of ['> minimum', '<= minimum', '!= minimum', 'minimum', 'not(minimum)', '[minimum..100]']) {
      expect(validateCell(cell)).toBeUndefined();
    }
  });

  it('raises nothing that stops the table being saved', () => {
    expect(findProblems('FIRST', [score, minimum], [verdict], passMark)).toEqual([]);
  });

  it('says in its hint which column it compares with', () => {
    expect(describeCell('> minimum', 'Score', headings)).toBe('Score is more than Minimum');
    expect(describeCell('<= minimum', 'Score', headings)).toBe('Score is at most Minimum');
    expect(describeCell('minimum', 'Score', headings)).toBe('Score is the same as Minimum');
    expect(describeCell('[minimum..100]', 'Score', headings)).toBe('Score is from Minimum up to 100');
    // A word that names no column is still the word.
    expect(describeCell('GOLD', 'Tier', headings)).toBe('Tier is GOLD');
  });

  it('leaves the coverage check silent, and says why without calling the cell unreadable', () => {
    const report = findCoverageGaps([score, minimum], passMark);
    expect(report.gaps).toEqual([]);
    expect(report.notAnalysed).toEqual(['Score']);
    expect(whyNotChecked(report)).toBe(
      'Not checked: Score is compared with Minimum, whose value changes from case to case, so this check cannot tell whether every case is decided. Try it with the values you care about.',
    );
  });

  it('names a variable of the decision that no column reads', () => {
    const report = findCoverageGaps([score], [line('over', ['> credit_limit'], 'REVIEW'), line('rest', ['-'], 'AUTO')]);
    expect(whyNotChecked(report)).toStartWith('Not checked: Score is compared with credit_limit,');
  });

  it('does not send the author to Try it for a value Try it cannot set', () => {
    // Try it has a box for each condition of the table and nothing else, so
    // credit_limit would be missing there and the line would never match.
    const overLimit = [line('over', ['> credit_limit'], 'REVIEW'), line('rest', ['-'], 'AUTO')];
    expect(whyNotChecked(findCoverageGaps([score], overLimit))).toBe(
      'Not checked: Score is compared with credit_limit, whose value changes from case to case, so this check cannot tell whether every case is decided. Try it can only set the conditions of this table, not credit_limit.',
    );

    const band = [line('band', ['[minimum..limits.ceiling]', '-'], 'IN'), line('rest', ['-', '-'], 'OUT')];
    expect(whyNotChecked(findCoverageGaps([score, minimum], band))).toEndWith(
      'Try it can only set the conditions of this table, not limits.ceiling.',
    );
  });

  it('on its own is that column, not the word', () => {
    // Under "only one line may match", `minimum` and "minimum" were both read as
    // the word, so the check reported that they overlap and refused to save a
    // table whose first line compares Level with the Minimum column.
    const level: DecisionInputColumn = { id: 'i1', label: 'Level', expression: 'level', type: 'string' };
    const floor: DecisionInputColumn = { id: 'i2', label: 'Minimum', expression: 'minimum', type: 'string' };
    const lines = [line('same', ['minimum', '-'], 'SAME'), line('word', ['"minimum"', '-'], 'WORD')];

    expect(findProblems('UNIQUE', [level, floor], [verdict], lines)).toEqual([]);
    expect(findCoverageGaps([level, floor], lines).needsQuotes).toEqual([]);
  });
});

/**
 * `?` is how Camunda writes a condition's own value, as in `? > minimum`. It is
 * not part of the language the engine reads — the table fails when it runs —
 * so the cell says so while it is being typed.
 */
describe('a question mark in a condition', () => {
  it('is marked in its cell, with what to write instead', () => {
    expect(validateCell('? > minimum')).toBe('Leave out the ?: a condition already tests its own column, as in > minimum');
  });

  it('is fine inside quoted text', () => {
    expect(validateCell('"Why?"')).toBeUndefined();
  });
});
