import { describe, expect, it } from 'bun:test';

import { describeHours, slaReport, slaReportCsv, slaReportFromDeadlines, slaSummary, type ReportableTask } from './slaReport';
import { urgencyOf } from './taskUrgency';

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
      { name: 'ollie', breached: 2, worstHoursLate: 20, worstLateBy: '20 hours' },
      { name: 'Unassigned', breached: 2, worstHoursLate: 5, worstLateBy: '5 hours' },
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
      { name: 'Onboarding', breached: 2, worstHoursLate: 40, worstLateBy: '40 hours' },
      { name: 'Quotation approval', breached: 1, worstHoursLate: 10, worstLateBy: '10 hours' },
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

describe('withdrawn and late', () => {
  const now = new Date('2026-09-25T12:00:00Z');

  it('does not count a withdrawn task as breached', () => {
    const report = slaReport([{ id: 't1', name: 'Approve', status: 'canceled', dueDate: '2026-09-20T12:00:00Z' }], now);
    expect(report.breached).toHaveLength(0);
  });

  it('says how late a task is the same way the inbox does', () => {
    // Sixty hours read "3 days" in the inbox and "2 days" here.
    const task = { id: 't1', name: 'Approve', status: 'claimed', dueDate: '2026-09-23T00:00:00Z' };
    const report = slaReport([task], now);
    expect(slaSummary(report)).toContain(urgencyOf(task, now).label.replace('Overdue by ', ''));
  });
});

// The dashboard built the report from the first page of the task list: the
// newest 200 tasks of any status, from rows that name no process. With three
// late tasks and 205 newer ones, the page held none of the late ones and every
// task on it was "Unknown process". It is built from the server's read now.
describe('slaReportFromDeadlines', () => {
  const now = new Date('2026-09-25T12:00:00Z');
  const late = (id: string, due: string) => ({
    id,
    name: 'Review',
    node_id: 'review',
    status: 'unclaimed',
    due_date: due,
    process_key: 'quotation',
    process_name: 'Quotation approval',
  });

  it('names the process of each late task', () => {
    const report = slaReportFromDeadlines(
      { tasks: [late('t-1', '2026-09-20T12:00:00Z')], with_deadline: 1, without_deadline: 205 },
      now,
    );
    expect(report.breached.map((task) => task.processName)).toEqual(['Quotation approval']);
    expect(report.byProcess.map((group) => group.name)).toEqual(['Quotation approval']);
  });

  it('counts the open work with no deadline as the server does, since none of it is sent', () => {
    const report = slaReportFromDeadlines({ tasks: [], with_deadline: 0, without_deadline: 205 }, now);
    expect(report.withoutDeadline).toBe(205);
  });

  it('says how many later deadlines were not sent', () => {
    const report = slaReportFromDeadlines(
      { tasks: [late('t-1', '2026-09-20T12:00:00Z')], with_deadline: 501, without_deadline: 0 },
      now,
    );
    expect(report.laterDeadlinesNotShown).toBe(500);
    expect(slaSummary(report)).toContain('500 more open tasks have a later deadline and are not listed.');
  });

  it('falls back to the process key when the process has no name', () => {
    const task = { ...late('t-1', '2026-09-20T12:00:00Z'), process_name: '' };
    const report = slaReportFromDeadlines({ tasks: [task], with_deadline: 1 }, now);
    expect(report.breached[0].processName).toBe('quotation');
  });
});
