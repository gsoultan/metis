import { toCsv, type CsvValue } from './csv';
import { describeSpan, urgencyOf, type UrgencyInput } from './taskUrgency';

/**
 * Which work has missed its deadline, and whose it is.
 *
 * The inbox already says a task is overdue, one task at a time, to the person
 * holding it. That is the wrong reader for the question a manager asks at the
 * end of a week: how much is late, how late, and who is carrying it. Nothing
 * answered that, so the answer was "open the inbox and count".
 *
 * Due dates and priorities are already on the task, so this is arithmetic over
 * data the app has rather than anything new to store.
 */

/** One task, as this report needs it. */
export interface ReportableTask extends UrgencyInput {
  id: string;
  name?: string;
  nodeId?: string;
  assignee?: string | null;
  processName?: string;
}

/** One late or nearly-late piece of work. */
export interface BreachedTask {
  id: string;
  name: string;
  assignee: string;
  processName: string;
  dueDate: string;
  /** Whole hours past the deadline. Negative means it has not passed yet. */
  hoursLate: number;
  /** How late, in the words the inbox uses for the same task. */
  lateBy: string;
}

/** How much is late, per person or per process. */
export interface BreachGroup {
  name: string;
  breached: number;
  /** The worst one in this group, in whole hours. */
  worstHoursLate: number;
  /** The worst one in this group, in words. */
  worstLateBy: string;
}

export interface SlaReport {
  /** Tasks whose deadline has passed and which nobody has finished. */
  breached: BreachedTask[];
  /** Open tasks with a deadline still ahead of them. */
  dueSoon: number;
  /** Open tasks with no deadline at all — nothing to breach, nothing to report. */
  withoutDeadline: number;
  byAssignee: BreachGroup[];
  byProcess: BreachGroup[];
  /**
   * Open tasks with a deadline that the server did not send: past its limit,
   * so the ones with the latest deadlines. None of them is late unless every
   * one that was sent is.
   */
  laterDeadlinesNotShown?: number;
}

/** A project's open work as far as deadlines go, as the server reads it. */
export interface Deadlines {
  /** The open tasks with a due date, soonest first. */
  tasks?: DeadlineTask[];
  /** All of the project's open tasks with a due date, sent or not. */
  with_deadline?: number;
  /** All of the project's open tasks with no due date. */
  without_deadline?: number;
}

/** One open task with a due date, and the process it is part of. */
export interface DeadlineTask {
  id: string;
  name?: string;
  node_id?: string;
  status?: string;
  priority?: number;
  assignee?: string;
  due_date: string;
  process_key?: string;
  process_name?: string;
}

const HOUR = 60 * 60 * 1000;
const UNASSIGNED = 'Unassigned';

/**
 * Builds the report.
 *
 * "Breached" is decided by the same urgency rule the inbox uses rather than by
 * comparing dates here, so a task is never late in one place and on time in the
 * other.
 */
export function slaReport(tasks: readonly ReportableTask[], now: Date = new Date()): SlaReport {
  const breached: BreachedTask[] = [];
  let dueSoon = 0;
  let withoutDeadline = 0;

  for (const task of tasks) {
    const urgency = urgencyOf(task, now);
    if (urgency.level === 'done') continue;

    if (!task.dueDate) {
      withoutDeadline += 1;
      continue;
    }
    if (urgency.level !== 'overdue') {
      dueSoon += 1;
      continue;
    }

    const due = new Date(task.dueDate);
    const late = (now.getTime() - due.getTime()) / HOUR;
    breached.push({
      id: task.id,
      name: task.name ?? task.nodeId ?? task.id,
      assignee: task.assignee?.trim() ? task.assignee : UNASSIGNED,
      processName: task.processName?.trim() ? task.processName : 'Unknown process',
      dueDate: task.dueDate,
      hoursLate: Math.floor(late),
      lateBy: describeHours(late),
    });
  }

  // Worst first: the question is what to do about it, and that starts with the
  // oldest breach rather than the newest.
  breached.sort((a, b) => b.hoursLate - a.hoursLate || a.name.localeCompare(b.name));

  return {
    breached,
    dueSoon,
    withoutDeadline,
    byAssignee: groupBreaches(breached, (task) => task.assignee),
    byProcess: groupBreaches(breached, (task) => task.processName),
  };
}

/**
 * The report over what the server read: every open task with a deadline,
 * soonest first, each naming its process.
 *
 * The dashboard built it from the first page of the task list, which held the
 * newest 200 tasks of any status and named no process. In a project with 200
 * newer tasks the late ones were not on the page, and a late task that was
 * read "Unknown process".
 */
export function slaReportFromDeadlines(deadlines: Deadlines, now: Date = new Date()): SlaReport {
  const tasks = deadlines.tasks ?? [];
  const report = slaReport(
    tasks.map((task) => ({
      id: task.id,
      name: task.name,
      nodeId: task.node_id,
      status: task.status,
      priority: task.priority,
      dueDate: task.due_date,
      assignee: task.assignee || null,
      processName: task.process_name || task.process_key,
    })),
    now,
  );
  // Work with no deadline is counted by the server and never sent.
  report.withoutDeadline = deadlines.without_deadline ?? 0;
  const notShown = (deadlines.with_deadline ?? tasks.length) - tasks.length;
  if (notShown > 0) report.laterDeadlinesNotShown = notShown;
  return report;
}

function groupBreaches(breached: readonly BreachedTask[], key: (task: BreachedTask) => string): BreachGroup[] {
  const groups = new Map<string, BreachGroup>();
  for (const task of breached) {
    const name = key(task);
    const group = groups.get(name);
    if (group) {
      group.breached += 1;
      if (task.hoursLate > group.worstHoursLate) {
        group.worstHoursLate = task.hoursLate;
        group.worstLateBy = task.lateBy;
      }
      continue;
    }
    groups.set(name, { name, breached: 1, worstHoursLate: task.hoursLate, worstLateBy: task.lateBy });
  }
  return [...groups.values()].sort(
    (a, b) => b.breached - a.breached || b.worstHoursLate - a.worstHoursLate || a.name.localeCompare(b.name),
  );
}

/**
 * One sentence for the top of the card.
 *
 * Says the good news plainly too: "nothing is late" is the answer somebody is
 * hoping for and it should not be an empty table.
 */
export function slaSummary(report: SlaReport): string {
  if (report.breached.length === 0) {
    if (report.dueSoon === 0 && report.withoutDeadline === 0) {
      return 'No open work with a deadline.';
    }
    return `Nothing is late. ${report.dueSoon} open ${report.dueSoon === 1 ? 'task has' : 'tasks have'} a deadline still ahead.`;
  }
  const count = report.breached.length;
  const worst = report.breached[0];
  const late = `${count} ${count === 1 ? 'task is' : 'tasks are'} past their deadline, the oldest by ${worst.lateBy}.`;
  const notShown = report.laterDeadlinesNotShown ?? 0;
  return notShown > 0
    ? `${late} ${notShown} more open ${notShown === 1 ? 'task has' : 'tasks have'} a later deadline and ${notShown === 1 ? 'is' : 'are'} not listed.`
    : late;
}

/**
 * Hours as something readable — days once there are enough of them — in the
 * inbox's words, so the same lateness reads the same wherever it is shown.
 */
export function describeHours(hours: number): string {
  if (hours < 1) return 'less than an hour';
  return describeSpan(hours);
}

/** The report as a spreadsheet, one row per breached task. */
export function slaReportCsv(report: SlaReport): string {
  const headers = ['Task', 'Process', 'Assignee', 'Due', 'Hours late'];
  const rows: CsvValue[][] = report.breached.map((task) => [
    task.name,
    task.processName,
    task.assignee,
    task.dueDate,
    task.hoursLate,
  ]);
  return toCsv(headers, rows);
}
