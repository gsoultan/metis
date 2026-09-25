/**
 * Which lines of a decision table apply to the same case, and what that costs
 * under the table's hit policy.
 *
 * The check this replaces compared lines by their text, so it caught a line
 * pasted twice and nothing else. `> 10` beside `> 20` both apply to 25, and
 * under "Only one line may match" that fails the decision for every such case:
 * at runtime, in an instance, long after the table was saved. Lines are
 * compared here by what they match, read with the coverage analysis's matcher
 * and its samples, which stand for every value a table can tell apart.
 *
 * Like that analysis it says nothing it cannot back. A cell in notation the
 * matcher does not read makes the answer unknown, and unknown is not reported:
 * a false alarm teaches people to ignore the check.
 */
import type { TableProblem } from './decisionProblems';
import {
  hitPolicyOf,
  normalizeCell,
  parseOutputValue,
  type DecisionInputColumn,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from './decisionTable';
import { canMatch, covers, isWildcard, linesMeet, meetingExample, readColumns, type ColumnReading } from './lineComparison';
import { WorkBudget } from './sampleSets';

/**
 * Pairs are listed a few at a time. A table where every line overlaps every
 * other has a problem per pair, and fifty alerts bury the one sentence that
 * says what to do about them.
 */
const MAX_REPORTED_PAIRS = 5;

/**
 * The pairwise work one check may do, in words compared (see WorkBudget):
 * a few tens of milliseconds, whatever the table. Every pair of a table of two
 * thousand banded lines fits many times over; a table of thousands of lines
 * that all overlap one another does not, and is reported as checked in part.
 */
export const OVERLAP_WORK_BUDGET = 4_000_000;

/** Whether two lines give the same results. */
type SameResults = (a: number, b: number) => boolean;

export function findOverlaps(
  hitPolicy: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
  budgetUnits = OVERLAP_WORK_BUDGET,
): TableProblem[] {
  if (rules.length < 2) return [];
  const catchAlls = rules.flatMap((rule, index) => (isCatchAll(rule) ? [index] : []));
  const budget = new WorkBudget(budgetUnits);
  const read = () => readColumns(inputs, rules);

  switch (hitPolicy) {
    case 'FIRST':
      return [...catchAllHidesTheRest(catchAlls, rules.length), ...shadowedLines(read(), rules, budget, catchAlls[0])];
    case 'UNIQUE':
      return [...catchAllsApplyWithEverything(catchAlls), ...overlappingPairs(read(), rules, catchAlls, budget)];
    case 'ANY': {
      const same = sameResultsOf(rules, outputs);
      return [
        ...catchAllDisagreements(catchAlls, rules, same),
        ...disagreeingPairs(read(), rules, catchAlls, same, budget),
        ...repeatedConditions(rules, same),
      ];
    }
    default:
      // Ranking and collecting policies are built for lines that overlap. A
      // line written twice is still worth a word: it is usually a paste.
      return repeatedConditions(rules, () => true);
  }
}

/** A line with nothing in any condition: it applies to every case. */
function isCatchAll(rule: DecisionRuleRow): boolean {
  return rule.input_entries.every(isWildcard);
}

/**
 * Visits every pair of lines that are both real candidates for a case, the
 * later line outermost, until the budget runs out, and says how many lines from
 * the top were compared with each other: all of them, unless it stopped short.
 *
 * Pairs are walked rather than listed first. A list of every pair of two
 * thousand lines is two million arrays, made before the first comparison.
 */
function eachCandidatePair(
  columns: ColumnReading[],
  rules: DecisionRuleRow[],
  excluded: number[],
  budget: WorkBudget,
  visit: (earlier: number, later: number) => void,
): number {
  const skipped = new Set(excluded);
  const candidates = rules.flatMap((_, index) => (!skipped.has(index) && canMatch(columns, index) ? [index] : []));
  for (let position = 1; position < candidates.length; position += 1) {
    for (let before = 0; before < position; before += 1) visit(candidates[before], candidates[position]);
    if (budget.exhausted && position < candidates.length - 1) return candidates[position] + 1;
  }
  return rules.length;
}

/** Said when the walk stopped short: what was compared, and what it means for the rest. */
function stoppedShort(checked: number, lineCount: number): TableProblem[] {
  if (checked >= lineCount) return [];
  return [
    {
      severity: 'warning',
      message: `Only lines 1 to ${checked} were checked against each other: the table is too long to compare every pair of lines, so a problem further down would not be listed here.`,
    },
  ];
}

function when(example: string | undefined): string {
  return example ? `when ${example}` : 'to some of the same cases';
}

function overlappingPairs(
  columns: ColumnReading[],
  rules: DecisionRuleRow[],
  catchAlls: number[],
  budget: WorkBudget,
): TableProblem[] {
  const found = new FewPairs();
  const checked = eachCandidatePair(columns, rules, catchAlls, budget, (a, b) => {
    if (linesMeet(columns, a, b, budget) !== true) return;
    found.add(() => ({
      severity: 'error',
      message: `Lines ${a + 1} and ${b + 1} both apply ${when(meetingExample(columns, a, b))}, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.`,
    }));
  });
  return [
    ...found.listed((rest) => `${rest} more ${rest === 1 ? 'pair' : 'pairs'} of lines overlap the same way.`),
    ...stoppedShort(checked, rules.length),
  ];
}

function disagreeingPairs(
  columns: ColumnReading[],
  rules: DecisionRuleRow[],
  catchAlls: number[],
  same: SameResults,
  budget: WorkBudget,
): TableProblem[] {
  const found = new FewPairs();
  const checked = eachCandidatePair(columns, rules, catchAlls, budget, (a, b) => {
    if (same(a, b) || linesMeet(columns, a, b, budget) !== true) return;
    found.add(() => ({
      severity: 'error',
      message: `Lines ${a + 1} and ${b + 1} both apply ${when(meetingExample(columns, a, b))} but give different results. Lines that apply together must agree, so the decision fails there.`,
    }));
  });
  return [
    ...found.listed((rest) => `${rest} more ${rest === 1 ? 'pair' : 'pairs'} of lines disagree the same way.`),
    ...stoppedShort(checked, rules.length),
  ];
}

/**
 * Lines an earlier line hides completely, when the first match wins.
 *
 * Lines below a catch-all are left to its own warning, which already says none
 * of them can be reached.
 */
function shadowedLines(
  columns: ColumnReading[],
  rules: DecisionRuleRow[],
  budget: WorkBudget,
  firstCatchAll = rules.length,
): TableProblem[] {
  const found = new FewPairs();
  let checked = firstCatchAll;
  for (let line = 1; line < firstCatchAll; line += 1) {
    const hider = canMatch(columns, line) ? earlierLineCovering(columns, line, budget) : -1;
    if (hider >= 0) {
      found.add(() => ({
        severity: 'warning',
        message: `Line ${line + 1} can never be reached: line ${hider + 1} comes before it and applies to every case it does. Move it above line ${hider + 1}, or remove it.`,
      }));
    }
    if (budget.exhausted && line < firstCatchAll - 1) {
      checked = line + 1;
      break;
    }
  }
  return [
    ...found.listed((rest) => `${rest} more ${rest === 1 ? 'line' : 'lines'} can never be reached for the same reason.`),
    ...stoppedShort(checked, firstCatchAll),
  ];
}

function earlierLineCovering(columns: ColumnReading[], line: number, budget: WorkBudget): number {
  for (let earlier = 0; earlier < line; earlier += 1) {
    if (covers(columns, earlier, line, budget)) return earlier;
  }
  return -1;
}

/** Lines whose conditions say the same thing, where the policy tolerates overlap. */
function repeatedConditions(rules: DecisionRuleRow[], alsoSame: SameResults): TableProblem[] {
  const seen = new Map<string, number>();
  const problems: TableProblem[] = [];
  rules.forEach((rule, index) => {
    const signature = rule.input_entries.map(normalizeCell).join('\u0000');
    const previous = seen.get(signature);
    if (previous === undefined) {
      seen.set(signature, index);
    } else if (alsoSame(previous, index)) {
      problems.push({ severity: 'warning', message: `Lines ${previous + 1} and ${index + 1} test the same conditions.` });
    }
  });
  return problems;
}

/**
 * The first few problems, and a count of the rest. The rest are counted, not
 * written out: in a long table of lines that all overlap there is one per pair,
 * and writing millions of sentences nobody reads was most of the check's time.
 */
class FewPairs {
  private readonly first: TableProblem[] = [];
  private more = 0;

  add(problem: () => TableProblem): void {
    if (this.first.length < MAX_REPORTED_PAIRS) this.first.push(problem());
    else this.more += 1;
  }

  listed(countTheRest: (rest: number) => string): TableProblem[] {
    if (this.more === 0) return this.first;
    return [...this.first, { severity: this.first[0].severity, message: countTheRest(this.more) }];
  }
}

/**
 * Only when the first match wins does a catch-all hide the lines below it, and
 * only the first catch-all needs saying: everything under it, other catch-alls
 * included, is already out of reach.
 */
function catchAllHidesTheRest(catchAlls: number[], lineCount: number): TableProblem[] {
  const first = catchAlls[0];
  if (first === undefined || first === lineCount - 1) return [];
  return [
    {
      severity: 'warning',
      message: `Line ${first + 1} matches everything, so no line below it can ever be reached. Catch-all lines belong last.`,
    },
  ];
}

/** Under UNIQUE a catch-all applies alongside every other line, wherever it sits. */
function catchAllsApplyWithEverything(catchAlls: number[]): TableProblem[] {
  const firstWins = hitPolicyOf('FIRST')?.label ?? 'FIRST';
  return catchAlls.map((index) => ({
    severity: 'error',
    message: `Line ${index + 1} matches everything, so it applies alongside every other line, and only one line may match: every case another line decides fails the decision. Give it conditions of its own, or choose “${firstWins}” and keep it last.`,
  }));
}

/**
 * Under ANY the lines a catch-all applies alongside must give its result. Two
 * catch-alls that disagree are named once, by the first of them.
 */
function catchAllDisagreements(catchAlls: number[], rules: DecisionRuleRow[], same: SameResults): TableProblem[] {
  return catchAlls.flatMap((catchAll): TableProblem[] => {
    const alreadyNamed = (index: number) => index < catchAll && catchAlls.includes(index);
    const disagreeing = rules.flatMap((_, index) =>
      index !== catchAll && !alreadyNamed(index) && !same(catchAll, index) ? [index + 1] : [],
    );
    if (disagreeing.length === 0) return [];
    const which = disagreeing.length === 1 ? `line ${disagreeing[0]} gives` : `lines ${joinNumbers(disagreeing)} give`;
    return [
      {
        severity: 'error',
        message: `Line ${catchAll + 1} matches everything, so it applies alongside every other line, and ${which} a different result. Lines that apply together must agree, so those cases fail the decision.`,
      },
    ];
  });
}

/**
 * Whether two lines produce the same values, compared as the engine compares
 * them: as stored, so `"LOW"` and `LOW` are the same result.
 *
 * Each line's results are read once. Comparing a pair used to parse both
 * lines' cells again, which under ANY was every pair of the table.
 */
function sameResultsOf(rules: DecisionRuleRow[], outputs: DecisionOutputColumn[]): SameResults {
  const results = rules.map((rule) =>
    JSON.stringify(outputs.map((output, index) => parseOutputValue(rule.output_entries[index] ?? '', output.type))),
  );
  return (a, b) => results[a] === results[b];
}

function joinNumbers(numbers: number[]): string {
  if (numbers.length === 1) return String(numbers[0]);
  return `${numbers.slice(0, -1).join(', ')} and ${numbers[numbers.length - 1]}`;
}
