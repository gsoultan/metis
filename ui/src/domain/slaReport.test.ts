import { describe, expect, it } from 'bun:test';

import { describeHours, slaReport, slaReportCsv, slaSummary, type ReportableTask } from './slaReport';

const NOW = new Date('2026-09-23T12:00:00Z');

const hoursAgo = (hours: number) => new Date(NOW.getTime() - hours * 60 * 60 * 1000).toISOString();
const hoursAhead = (hours: number) => new Date(NOW.getTime() + hours * 60 * 60 * 1000).toISOString();

const task = (over: Partial<ReportableTask> & { id: string }): ReportableTask => ({
  status: 'unclaimed',
  processName: 'Quotation approval',
  ...over,
});

describe('slaReport', () => {
  it('reports what is past its deadline and how far past', () => {
    const report = slaReport(
      [
        task({ id: '1', name: 'Operations approve', dueDate: hoursAgo(30), assignee: 'ollie' }),
        task({ id: '2', name: 'Sales approve', dueDate: hoursAgo(3), assignee: 'sasha' }),
      ],
      NOW,
    );

    // Worst first: what to do about it starts with the oldest breach.
    expect(report.breached.map((b) => b.name)).toEqual(['Operations approve', 'Sales approve']);
    expect(report.breached[0].hoursLate).toBe(30);
  });

  it('does not count work that is finished', () => {
    const report = slaReport(
      [task({ id: '1', name: 'Done long ago', dueDate: hoursAgo(99), status: 'completed' })],
      NOW,
    );
    expect(report.breached).toEqual([]);
  });

  it('separates work with a deadline ahead from work with none at all', () => {
    const report = slaReport(
      [
        task({ id: '1', dueDate: hoursAhead(5) }),
        task({ id: '2' }),
        task({ id: '3' }),
      ],
      NOW,
    );
    expect(report.dueSoon).toBe(1);
    // Nothing to breach, so nothing to report — but not silently dropped
    // either, because "why is my task not in this?" has to have an answer.
    expect(report.withoutDeadline).toBe(2);
    expect(report.breached).toEqual([]);
  });

  it('says whose it is, and calls nobody Unassigned rather than blank', () => {
    const report = slaReport(
      [
        task({ id: '1', dueDate: hoursAgo(10), assignee: 'ollie' }),
        task({ id: '2', dueDate: hoursAgo(20), assignee: 'ollie' }),
        task({ id: '3', dueDate: hoursAgo(5), assignee: null }),
        task({ id: '4', dueDate: hoursAgo(1), assignee: '   ' }),
      ],
      NOW,
    );

    expect(report.byAssignee).toEqual([
      { name: 'ollie', breached: 2, worstHoursLate: 20 },
      { name: 'Unassigned', breached: 2, worstHoursLate: 5 },
    ]);
  });

  it('groups by process too, because one late process is a different problem', () => {
    const report = slaReport(
      [
        task({ id: '1', dueDate: hoursAgo(10), processName: 'Quotation approval' }),
        task({ id: '2', dueDate: hoursAgo(40), processName: 'Onboarding' }),
        task({ id: '3', dueDate: hoursAgo(2), processName: 'Onboarding' }),
      ],
      NOW,
    );
    expect(report.byProcess).toEqual([
      { name: 'Onboarding', breached: 2, worstHoursLate: 40 },
      { name: 'Quotation approval', breached: 1, worstHoursLate: 10 },
    ]);
  });
});

describe('slaSummary', () => {
  it('says the good news plainly', () => {
    expect(slaSummary(slaReport([task({ id: '1', dueDate: hoursAhead(4) })], NOW))).toBe(
      'Nothing is late. 1 open task has a deadline still ahead.',
    );
  });

  it('says so when there is nothing with a deadline at all', () => {
    expect(slaSummary(slaReport([], NOW))).toBe('No open work with a deadline.');
  });

  it('leads with how many and how bad', () => {
    expect(
      slaSummary(slaReport([task({ id: '1', name: 'Approve', dueDate: hoursAgo(72) })], NOW)),
    ).toBe('1 task is past their deadline, the oldest by 3 days.');
  });
});

describe('describeHours', () => {
  it('reads as a person would say it', () => {
    expect(describeHours(0)).toBe('less than an hour');
    expect(describeHours(1)).toBe('1 hour');
    expect(describeHours(5)).toBe('5 hours');
    expect(describeHours(72)).toBe('3 days');
  });
});

describe('slaReportCsv', () => {
  it('writes a row per breached task', () => {
    const csv = slaReportCsv(
      slaReport([task({ id: '1', name: 'Approve', dueDate: hoursAgo(2), assignee: 'ollie' })], NOW),
    );
    const lines = csv.split('\r\n');
    expect(lines[0]).toBe('Task,Process,Assignee,Due,Hours late');
    expect(lines[1]).toContain('Approve');
    expect(lines[1]).toContain('ollie');
    expect(lines[1].endsWith(',2')).toBe(true);
  });

  it('escapes a task name that would break the columns or run as a formula', () => {
    const csv = slaReportCsv(
      slaReport(
        [task({ id: '1', name: '=cmd|"/c calc"!A1, urgent', dueDate: hoursAgo(1), assignee: 'ollie' })],
        NOW,
      ),
    );
    // Quoted because of the comma, and prefixed because of the leading '='.
    expect(csv).toContain(`"'=cmd|""/c calc""!A1, urgent"`);
  });

  it('writes a header even when nothing is late', () => {
    expect(slaReportCsv(slaReport([], NOW))).toBe('Task,Process,Assignee,Due,Hours late');
  });
});
