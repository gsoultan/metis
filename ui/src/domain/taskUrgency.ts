/**
 * How urgent a task is, and why.
 *
 * The inbox is where somebody spends their working day, and the question they
 * ask it is always the same: what should I do next. A list sorted by creation
 * date does not answer that, and a "Priority: 70" column answers it only for
 * whoever remembers what 70 means.
 *
 * So urgency is computed once, from the two things that actually make a task
 * urgent — it is late, or it is important — and rendered as weight: colour,
 * order, a word. The thresholds live here rather than being written inline in
 * five places, which is where they were and where they had already drifted
 * apart.
 */

/** As much of a task as urgency depends on. */
export interface UrgencyInput {
  dueDate?: string | null;
  priority?: number;
  status?: string;
}

export type UrgencyLevel = 'overdue' | 'today' | 'soon' | 'important' | 'normal' | 'done';

export interface Urgency {
  level: UrgencyLevel;
  /** What to say about it, in words somebody can act on. */
  label: string;
  /** A Mantine colour, so the whole app agrees on what "late" looks like. */
  color: string;
  /**
   * Higher sorts first. A number rather than an enum because the comparator has
   * to break ties inside a level — two overdue tasks are not equally overdue.
   */
  weight: number;
}

/**
 * What counts as high priority.
 *
 * BPMN priority is an unbounded integer and every engine picks its own
 * convention. This follows the one the diagrams in this repository use: 0 is
 * unset, 50 is the middle, anything above it is deliberate.
 */
export const HIGH_PRIORITY = 50;

/** Within this long, "soon" stops being an abstraction. */
const SOON_HOURS = 24;

/**
 * Rates one task.
 *
 * `now` is passed rather than read, so a test can say what time it is and so a
 * list rendered in one pass rates every task against the same instant — a
 * boundary crossing mid-render would sort two identical tasks differently.
 */
export function urgencyOf(task: UrgencyInput, now: Date = new Date()): Urgency {
  if (task.status === 'completed') {
    return { level: 'done', label: 'Done', color: 'green', weight: 0 };
  }

  const priority = task.priority ?? 0;
  const important = priority >= HIGH_PRIORITY;

  if (task.dueDate) {
    const due = new Date(task.dueDate);
    if (!Number.isNaN(due.getTime())) {
      const hoursLeft = (due.getTime() - now.getTime()) / 3_600_000;

      if (hoursLeft < 0) {
        // Weight grows with lateness, so a week-late task outranks an
        // hour-late one — the comparator needs to break ties inside a level.
        return {
          level: 'overdue',
          label: `Overdue by ${describeSpan(-hoursLeft)}`,
          color: 'red',
          weight: 1000 + Math.min(-hoursLeft, 720) + (important ? 100 : 0),
        };
      }
      if (isSameDay(due, now)) {
        return { level: 'today', label: 'Due today', color: 'orange', weight: 800 - hoursLeft + (important ? 100 : 0) };
      }
      if (hoursLeft <= SOON_HOURS) {
        return { level: 'soon', label: `Due in ${describeSpan(hoursLeft)}`, color: 'yellow', weight: 600 - hoursLeft };
      }
    }
  }

  if (important) {
    return { level: 'important', label: 'High priority', color: 'grape', weight: 400 + priority };
  }
  return { level: 'normal', label: '', color: 'gray', weight: priority };
}

/**
 * Orders tasks by what should be done next.
 *
 * Urgency first, then the older task, because between two equally urgent things
 * the one that has been waiting longer is the one somebody is waiting on.
 */
export function byUrgency<T extends UrgencyInput & { createdAt?: string }>(now: Date = new Date()) {
  return (a: T, b: T): number => {
    const difference = urgencyOf(b, now).weight - urgencyOf(a, now).weight;
    if (difference !== 0) return difference;
    return new Date(a.createdAt ?? 0).getTime() - new Date(b.createdAt ?? 0).getTime();
  };
}

/** Counts what is late and what is nearly late, for a heading. */
export function countUrgent<T extends UrgencyInput>(tasks: T[], now: Date = new Date()) {
  let overdue = 0;
  let dueToday = 0;
  for (const task of tasks) {
    const level = urgencyOf(task, now).level;
    if (level === 'overdue') overdue += 1;
    if (level === 'today') dueToday += 1;
  }
  return { overdue, dueToday };
}

function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
  );
}

/** "3 days", "5 hours", "20 minutes" — never "0.21 days". */
function describeSpan(hours: number): string {
  if (hours >= 48) return `${Math.round(hours / 24)} days`;
  if (hours >= 1) {
    const rounded = Math.round(hours);
    return `${rounded} hour${rounded === 1 ? '' : 's'}`;
  }
  const minutes = Math.max(1, Math.round(hours * 60));
  return `${minutes} minute${minutes === 1 ? '' : 's'}`;
}
