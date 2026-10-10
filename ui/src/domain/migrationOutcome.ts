import type { Values } from '../i18n/translate';
import type { ApiMigrationPlan, ApiNodeAction, ApiPassedOver, ApiPendingApproval, ApiPlannedNodeAction } from '../services/types';
import { instanceReference } from './instanceList';
import { heldTasksAffected, migrationRequestKey } from './instanceMigration';
import { versionPair } from './migrationDraft';

/**
 * What an apply did, in words, from the server's own answer.
 *
 * The toast after pressing "Move" said "Moved — N instances now run on vX"
 * whatever came back. N was the preview's count, not the apply's; a reply that
 * said it had applied nothing still read as a move; an apply over instances
 * that had all finished in the meantime still claimed N of them; and instances
 * that were cancelled or held rather than moved were counted as running on the
 * new version. Everything here is read from the reply: `applied`, and the plan
 * the server worked out immediately before it wrote anything.
 *
 * Two answers were then read as something they were not. An apply that needs
 * a second administrator is not made: it is stored as a request, and answered
 * with `applied: false` — which read as "Nothing was moved", to somebody whose
 * migration was in fact waiting for a colleague. And a run that left
 * instances on the old version said so in `passed_over`, which nothing read:
 * a run that moved two of three said three had moved. Both are read here, from
 * `pending_approval` and from the list and its count, and said through the
 * catalogues.
 */

/** A catalogue's `t`, as the provider hands it out. */
export type Translate = (key: string, values?: Values) => string;

/** The server's answer to an apply that did not fail outright. */
export interface MigrationReply {
  plan?: ApiMigrationPlan;
  applied: boolean;
  /** The request the apply became, when it needs a second administrator. */
  pending_approval?: ApiPendingApproval;
  /** The instances the run did not move: the first two hundred. */
  passed_over?: ApiPassedOver[];
  /** How many it did not move, listed or not. */
  passed_over_in_all?: number;
}

/** What to tell somebody once an apply has answered. */
export interface MigrationNotice {
  title: string;
  message: string;
  color: 'green' | 'gray' | 'yellow' | 'blue' | 'red';
  /** Whether the dialog is done with. False keeps it open for another look. */
  closes: boolean;
  /**
   * As a toast, whether it stays until it is dismissed. For what is said
   * nowhere else once the toast has gone: a request's reference, an answer
   * that could not be read, a refusal that arrived after the dialog closed.
   */
  stays?: boolean;
}

export function migrationNotice(reply: MigrationReply, target: number): MigrationNotice {
  if (!reply.applied || !reply.plan) {
    return {
      title: 'Nothing was moved',
      message: 'The server worked out the plan but did not apply it, so every instance is where it was.',
      color: 'yellow',
      closes: false,
    };
  }
  const plan = reply.plan;
  const source = plan.source_version;
  if (plan.instances === 0) {
    return {
      title: 'Nothing to move',
      message: `Nothing was running on v${source} any more, so no instance changed version.`,
      color: 'gray',
      closes: true,
    };
  }
  const decided = plan.actions ?? [];
  // "Still running" is doing work: a skip that reaches the end finishes the
  // instance on the old version, and a cancelled one has ended.
  const lines = decided.length === 0
    ? [movedLine(plan.instances, source, target)]
    : [`The ${plan.instances} ${plan.instances === 1 ? 'instance' : 'instances'} on v${source} were dealt with.`,
      ...decided.map((action) => decisionLine(action, source)),
      `Every other instance still running now runs on v${target}.`];
  const held = heldTasksAffected(plan);
  if (held > 0) lines.push(`${held === 1 ? '1 task' : `${held} tasks`} somebody was holding went back to the queue.`);
  return {
    title: decided.length === 0 ? `Moved to v${target}` : 'Migration applied',
    message: lines.join(' '),
    color: 'green',
    closes: true,
  };
}

function movedLine(instances: number, source: number, target: number): string {
  return instances === 1
    ? `1 instance that was running on v${source} now runs on v${target}.`
    : `${instances} instances that were running on v${source} now run on v${target}.`;
}

/**
 * What a decision did to the instances waiting at its step. Worded from what
 * the server does with each: a cancel ends the instance where it stands and it
 * keeps the version it ran; a hold leaves it on the old version as an
 * incident; a skip advances past the step, and the instance then moves.
 */
function decisionLine(action: ApiPlannedNodeAction, source: number): string {
  const step = `"${action.name || action.node_id}"`;
  switch (action.kind) {
    case 'cancel':
      return `Those waiting at ${step} were ended where they were, and keep v${source}.`;
    case 'hold':
      return `Those waiting at ${step} were left on v${source} for somebody to decide.`;
    default:
      return `Those waiting at ${step} skipped it and carried on.`;
  }
}

/**
 * A reply is the server's, and a bundle may be older or newer than the server
 * that wrote it. What is read here is read as what it is: a string where a
 * string is expected, a list where a list is. Anything else is left out, and
 * nothing is thrown — an exception on the way to saying "sent for approval"
 * would be shown as an apply the server did not confirm.
 */
function words(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function listed(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** The sentences of a list of sentences: those that are words. */
export function sentences(value: unknown): string[] {
  return listed(value).filter((one): one is string => words(one) !== '');
}

function field(of: unknown, name: string): unknown {
  return of !== null && typeof of === 'object' ? (of as Record<string, unknown>)[name] : undefined;
}

/**
 * How many instances the run did not move.
 *
 * The server's count, because its list stops at two hundred. The list's own
 * length stands in for a server that sends no count, and is believed over a
 * count smaller than itself.
 */
export function passedOverCount(reply: MigrationReply): number {
  const counted: unknown = reply.passed_over_in_all;
  return Math.max(typeof counted === 'number' && Number.isFinite(counted) ? counted : 0, listed(reply.passed_over).length);
}

/**
 * Whether the server answered with nothing this can read.
 *
 * Every reply the route writes carries the plan. One without it — and without
 * a request, and without anybody passed over — is a success whose body was
 * not the route's: cut short, or rewritten on the way. It is not "nothing was
 * moved": the apply may have been made, or sent, and nobody here can tell.
 */
function unreadable(reply: MigrationReply): boolean {
  return !reply.plan && !reply.pending_approval && passedOverCount(reply) === 0;
}

/**
 * What an apply did, in the reader's language, whatever the answer was: the
 * sentence the dialog says it in.
 *
 * In this order, because each is the truer statement when two hold. A request
 * that waits comes first and is read from `pending_approval` alone — not from
 * a status code, and not from `applied`, which is false on it exactly as it
 * is on an apply that did nothing. Then instances passed over: `applied` is
 * true beside them when the run changed anything at all, and false when it
 * passed every one over. Then an answer that could not be read. Every other
 * answer is migrationNotice's, unchanged.
 *
 * Neither count of instances passed over is a count of what moved. The plan's
 * count is of the instances it listed, and the run may have passed over
 * instances that arrived after it; the difference of the two is not a number
 * the server gave. And "this run moved no instance" is said of the run: an
 * instance in its list may have been moved by another.
 */
export function outcomeNotice(
  reply: MigrationReply,
  target: number,
  t: Translate,
  formatDate: (iso: string) => string,
): MigrationNotice {
  if (reply.pending_approval) {
    return { title: t('migration.pendingTitle'), message: pendingLines(reply.pending_approval, t, formatDate).join(' '), color: 'blue', closes: false };
  }
  const passedOver = passedOverCount(reply);
  if (passedOver > 0) {
    return {
      title: passedOverTitle(reply, t),
      message: t('migration.passedOverSummary', { count: passedOver }),
      color: 'yellow',
      closes: false,
    };
  }
  if (unreadable(reply)) {
    return { title: t('migration.unreadableTitle'), message: t('migration.unreadableMessage'), color: 'yellow', closes: false, stays: true };
  }
  return migrationNotice(reply, target);
}

function passedOverTitle(reply: MigrationReply, t: Translate): string {
  return t(reply.applied ? 'migration.passedOverSomeTitle' : 'migration.passedOverAllTitle');
}

/**
 * Who asked, the rule, and until when: a sentence each, and only those the
 * reply has the words for. The rule does not say who else approves — an
 * organization set up as having one administrator approves its own.
 */
function pendingLines(pending: unknown, t: Translate, formatDate: (iso: string) => string): string[] {
  const lines: string[] = [];
  const asked = words(field(pending, 'requested_by'));
  if (asked !== '') lines.push(t('migration.pendingAskedBy', { name: asked }));
  lines.push(t('migration.pendingMessage'));
  const until = words(field(pending, 'expires_at'));
  const date = until === '' ? '' : formatDate(until);
  if (date !== '') lines.push(t('migration.pendingExpires', { date }));
  return lines;
}

/**
 * Why one instance was not moved, in the reader's language.
 *
 * The server sends the cause as a code, the steps it is about by name, and its
 * own English sentence. The sentence here is the catalogue's for that cause;
 * the server's is what is said whenever the catalogue's cannot be said whole —
 * a cause this bundle has no words for (`t` answers the key itself), a cause
 * that names steps with none sent, or no version to say the instance stays on.
 * A sentence with a hole in it says less than the English one.
 *
 * A step's name is the modeller's and is said as it is written: it reaches
 * the sentence as a value, so braces in it are not placeholders.
 */
export function passedOverLine(entry: ApiPassedOver, sourceVersion: number | undefined, t: Translate): string {
  const reason = words(field(entry, 'reason'));
  const cause = words(field(entry, 'cause'));
  if (cause === '' || sourceVersion === undefined) return reason;
  const key = `migration.passedOver.${cause}`;
  const steps = stepsNamed(entry, t);
  // `steps` is left out rather than sent blank: `t` leaves a placeholder it
  // was given nothing for as written, which is how the hole is seen. Asked of
  // the catalogue's sentence, not of the finished one — a step may be named
  // "{steps}".
  if (steps === '') {
    const bare = t(key, { version: sourceVersion });
    return bare === key || bare.includes('{steps}') ? reason : bare;
  }
  const line = t(key, { steps, version: sourceVersion });
  return line === key ? reason : line;
}

/** `"Legal review", "Credit check" and 15 more`: the steps as people know them. */
function stepsNamed(entry: ApiPassedOver, t: Translate): string {
  const steps = listed(field(entry, 'steps'));
  const names = steps
    .map((step) => words(field(step, 'name')) || words(field(step, 'node_id')))
    .filter((name) => name !== '')
    .map((name) => `"${name}"`)
    .join(', ');
  const counted = field(entry, 'steps_in_all');
  const more = (typeof counted === 'number' && Number.isFinite(counted) ? counted : steps.length) - steps.length;
  return names !== '' && more > 0 ? `${names} ${t('migration.passedOverStepsMore', { count: more })}` : names;
}

/**
 * When a request stops waiting, as the reader's language writes a date, in
 * the reader's own time zone and naming it: the administrator who asked and
 * the one who approves may not be in the same place, and "4:12 PM" alone is
 * a different moment for each.
 *
 * Never throws: this is said of a request that has been sent, and an
 * exception on the way to saying it would be reported as an apply the server
 * did not confirm. A time that cannot be read is answered with nothing, for
 * the sentence to be left out, and a language the platform does not know is
 * written as English writes it.
 *
 * `timeZone` is for a test to pin the answer; the dialog leaves it out.
 */
export function formatExpiry(iso: string, locale: string, timeZone?: string): string {
  if (typeof iso !== 'string' || iso.trim() === '') return '';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '';
  // Spelled out: `dateStyle` and `timeStyle` cannot be asked for together
  // with the zone's name.
  const options: Intl.DateTimeFormatOptions = {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZoneName: 'short',
    timeZone,
  };
  try {
    return new Intl.DateTimeFormat(locale, options).format(at);
  } catch {
    return new Intl.DateTimeFormat('en', options).format(at);
  }
}

/**
 * How many passed-over instances the dialog lists at most.
 *
 * The server stops at the same number. Held here as well so that the length
 * of what is drawn is this module's to bound, whatever a reply carries.
 */
export const MAX_PASSED_OVER_SHOWN = 200;

/** One instance that was not moved, as a line of the list. */
export interface PassedOverRow {
  /** Unique in the list. Not shown. */
  key: string;
  /** "Instance #6789AB": the reference the instance list shows it by. */
  instance: string;
  why: string;
}

/** A sentence that names a request, with the name apart so it can be selected alone. */
export interface ReferenceLine {
  before: string;
  /** The request's id: all there is to name a request by. */
  value: string;
  after: string;
}

/** Everything an apply's answer gives the dialog to say. */
export interface MigrationOutcome {
  /** What the dialog says it in. For an answer that was always a toast, the toast. */
  notice: MigrationNotice;
  /**
   * What a toast says of it when the dialog cannot keep it: closed, or
   * showing something else by the time the answer came. Worded for a toast —
   * whole, with nothing "below" it.
   */
  toast: MigrationNotice;
  /** A request waits on a second administrator: nothing here is left to apply. */
  waits: boolean;
  /** The server answered and the answer could not be read. */
  unread: boolean;
  /** How a request that waits is approved, there being no screen for it. */
  how: string | null;
  /** The request's reference, in its sentence. */
  reference: ReferenceLine | null;
  /** What the list under the notice is a list of. */
  listTitle: string;
  /** Why a second administrator is asked, as the server's sentences. */
  reasons: string[];
  /** The instances the run did not move, each with why. */
  passedOver: PassedOverRow[];
  /** "and 140 more instances", for those the server counted and did not list. */
  more: string | null;
}

export function migrationOutcome(
  reply: MigrationReply,
  target: number,
  t: Translate,
  formatDate: (iso: string) => string,
): MigrationOutcome {
  const notice = outcomeNotice(reply, target, t, formatDate);
  if (reply.pending_approval) return waitingOutcome(reply.pending_approval, notice, sentTitle(reply, target, notice, t), t);
  const passedOver = passedOverCount(reply);
  const rows = listed(reply.passed_over).slice(0, MAX_PASSED_OVER_SHOWN) as ApiPassedOver[];
  const unlisted = passedOver - rows.length;
  return {
    notice,
    // A toast has no list under it, so it does not say "the list below". It
    // says how many, which is all that is true to say without the list: a
    // plan made again counts what is on the old version, not which instances
    // this run passed over or why.
    toast: passedOver > 0 ? { ...notice, message: t('migration.passedOverToast', { count: passedOver }) } : notice,
    waits: false,
    unread: passedOver === 0 && unreadable(reply),
    how: null,
    reference: null,
    listTitle: t('migration.passedOverListTitle'),
    reasons: [],
    passedOver: rows.map((entry, index) => ({
      key: `${index}:${words(field(entry, 'instance_id'))}`,
      instance: t('migration.passedOverInstance', { reference: instanceReference(words(field(entry, 'instance_id'))) }),
      why: passedOverLine(entry, reply.plan?.source_version, t),
    })),
    more: unlisted > 0 ? t('migration.passedOverMore', { count: unlisted }) : null,
  };
}

/**
 * A request that waits. Beside the notice: why it was asked for, how it is
 * approved — over the API, there being no screen yet — and the reference to
 * find it by. The toast says all of it in one, and stays until dismissed: once
 * the dialog has gone it is the only place the reference is.
 */
function waitingOutcome(pending: unknown, notice: MigrationNotice, toastTitle: string, t: Translate): MigrationOutcome {
  const how = t('migration.pendingHow');
  const reference = referenceLine(words(field(pending, 'request_id')), t);
  const whole = [notice.message, how];
  if (reference) whole.push(`${reference.before}${reference.value}${reference.after}`);
  return {
    notice,
    toast: { ...notice, title: toastTitle, message: whole.join(' '), stays: true },
    waits: true,
    unread: false,
    how,
    reference,
    listTitle: t('migration.pendingWhy'),
    reasons: sentences(field(pending, 'because')),
    passedOver: [],
    more: null,
  };
}

/**
 * The title of the toast a sent request leaves: the notice's, and the two
 * versions the request was for — as the server's reply names the one it was
 * sent from and the press named the one it was sent to. The dialog the toast
 * sits over may show another plan by then, edited while the apply was on its
 * way, with a button of its own; without the versions the toast could be
 * read as being about that one. A reply that names no source version says
 * the notice's title alone: nothing is invented.
 */
function sentTitle(reply: MigrationReply, target: number, notice: MigrationNotice, t: Translate): string {
  const source = reply.plan?.source_version;
  if (typeof source !== 'number' || !Number.isFinite(source)) return notice.title;
  return t('migration.pendingToastTitle', { source, target });
}

/** The catalogue's sentence about a reference, split round the reference itself. */
function referenceLine(reference: string, t: Translate): ReferenceLine | null {
  if (reference === '') return null;
  // Split where the catalogue put the placeholder, not where the value first
  // appears in the finished sentence.
  const mark = '\u0000';
  const [before, after = ''] = t('migration.pendingReference', { reference: mark }).split(mark);
  return { before, value: reference, after };
}

/**
 * Whether the dialog itself says this outcome, rather than a toast.
 *
 * A toast is gone in seconds and holds a sentence. Who a request waits on,
 * until when and why, which instances were not moved and why, and an answer
 * nobody could read, are for reading — so the dialog stays open and shows
 * them. Every other outcome is the toast it always was.
 */
export function saidInDialog(outcome: MigrationOutcome): boolean {
  return outcome.waits || outcome.unread || outcome.passedOver.length > 0 || outcome.more !== null;
}

/** What the dialog does once an apply has answered. */
export interface AfterApply {
  /** Keep the answer on screen, in the dialog. */
  keep: boolean;
  /** What to say in a toast: null exactly when the answer is kept. */
  toast: MigrationNotice | null;
  /** Work the plan out again for the same request. */
  replan: boolean;
  /** Close the dialog. */
  close: boolean;
}

/**
 * An answer is kept on screen or said in a toast: never both, and never
 * neither. Only a toast can close the dialog.
 *
 * `shown` is whether the screen will show it — asked with the test the
 * screen itself applies (outcomeOnScreen), of what is on screen when the
 * answer comes and not when the button was pressed. A form edited while the
 * apply was on its way no longer shows the request that was sent, and an
 * answer kept for a screen that will not show it is an answer said nowhere.
 * `open` is whether the dialog is still there at all. One closed while the
 * apply was on its way has nowhere to show the answer and is not closed
 * again: it may have been opened since, for another version.
 *
 * A run that left instances behind is planned again, as an apply that
 * stopped part-way is: the plan in hand counts instances the run has since
 * dealt with, and what is left is what the next apply is for. So is an answer
 * nobody could read. A request that waits changed nothing, so the plan that
 * was sent stays as it is.
 */
export function afterApply(outcome: MigrationOutcome, shown = true, open = true): AfterApply {
  const keep = open && shown && saidInDialog(outcome);
  return {
    keep,
    toast: keep ? null : outcome.toast,
    replan: keep && !outcome.waits,
    close: open && !keep && outcome.toast.closes,
  };
}

/**
 * A refusal, or a failure, that arrived after the dialog had closed.
 *
 * The dialog shows these as an alert, and there is no dialog. Left for the
 * next time it opens, the alert would be about a press nobody remembers —
 * perhaps for another version. In the server's words, which say what it did.
 */
export function failureToast(message: string, t: Translate): MigrationNotice {
  return { title: t('migration.failedTitle'), message, color: 'red', closes: false, stays: true };
}

/** An apply's answer, kept with what it answered. */
export interface AnsweredApply {
  /** The source and target it was for. See versionPair. */
  pair: string;
  /** The request it answered. See migrationRequestKey. */
  requestKey: string;
  reply: MigrationReply;
  /** The target version's number. */
  target: number;
}

/** What an apply sent, as the mutation that sent it keeps it. */
export interface SentApply {
  source: string;
  target: string;
  mapping: Record<string, string>;
  acknowledge?: string[];
  actions?: Record<string, ApiNodeAction>;
}

/**
 * The last apply's answer, from what was sent and what came back.
 *
 * Read from the apply itself rather than copied into the dialog's state: the
 * mutation already keeps what it sent and what it was answered, drops the
 * answer when the next apply starts, and is reset when the dialog closes — so
 * there is no second copy to fall out of step with it. Null while nothing has
 * answered, and for a refusal the reply carries: that is an error, and is
 * shown as one.
 *
 * `target` is the version number on screen, used when the reply names none.
 */
export function answeredApply(
  sent: SentApply | undefined,
  reply: (MigrationReply & { err?: string }) | undefined,
  target: number,
): AnsweredApply | null {
  if (!sent || !reply || reply.err) return null;
  return {
    pair: versionPair(sent.source, sent.target),
    requestKey: migrationRequestKey({ ...sent, acknowledge: sent.acknowledge ?? [], actions: sent.actions ?? {} }),
    reply,
    target: reply.plan?.target_version ?? target,
  };
}

/**
 * What the last apply did, for as long as it is true of what is on screen.
 *
 * Derived when the dialog renders, as the draft is, so nothing is shown for a
 * pair of versions it was not about. A request that waits is shown only while
 * the request on screen is the one that was sent: after an edit the dialog
 * describes another migration, which nobody has sent. The instances a run
 * passed over stay through edits — they are what the next mapping or decision
 * is written for — and so does an answer nobody could read, until the next
 * apply or until the dialog closes.
 */
export function outcomeOnScreen(
  answered: AnsweredApply | null,
  pair: string | null,
  requestKey: string | null,
  t: Translate,
  formatDate: (iso: string) => string,
): MigrationOutcome | null {
  if (answered === null || answered.pair !== pair) return null;
  const outcome = migrationOutcome(answered.reply, answered.target, t, formatDate);
  if (!saidInDialog(outcome)) return null;
  if (outcome.waits && answered.requestKey !== requestKey) return null;
  return outcome;
}
