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
  color: 'green' | 'gray' | 'yellow' | 'blue';
  /** Whether the dialog is done with. False keeps it open for another look. */
  closes: boolean;
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
 * How many instances the run did not move.
 *
 * The server's count, because its list stops at two hundred. The list's own
 * length stands in for a server that sends no count, and is believed over a
 * count smaller than itself.
 */
export function passedOverCount(reply: MigrationReply): number {
  return Math.max(reply.passed_over_in_all ?? 0, reply.passed_over?.length ?? 0);
}

/**
 * What an apply did, in the reader's language, whatever the answer was.
 *
 * In this order, because each is the truer statement when two hold. A request
 * that waits comes first and is read from `pending_approval` alone — not from
 * a status code, and not from `applied`, which is false on it exactly as it
 * is on an apply that did nothing. Then instances passed over: `applied` is
 * true beside them when the run changed anything at all, and false when it
 * passed every one over. Every other answer is migrationNotice's, unchanged.
 *
 * Neither new answer counts what moved. The plan's count is of the instances
 * it listed, and the run may have passed over instances that arrived after
 * it; the difference of the two is not a number the server gave.
 */
export function outcomeNotice(
  reply: MigrationReply,
  target: number,
  t: Translate,
  formatDate: (iso: string) => string,
): MigrationNotice {
  const pending = reply.pending_approval;
  if (pending) {
    const asked = (pending.requested_by ?? '').trim();
    const lines = asked === '' ? [] : [t('migration.pendingAskedBy', { name: asked })];
    lines.push(t('migration.pendingMessage', { date: formatDate(pending.expires_at) }));
    return { title: t('migration.pendingTitle'), message: lines.join(' '), color: 'blue', closes: false };
  }
  const passedOver = passedOverCount(reply);
  if (passedOver > 0) {
    return {
      title: t(reply.applied ? 'migration.passedOverSomeTitle' : 'migration.passedOverAllTitle'),
      message: t('migration.passedOverSummary', { count: passedOver }),
      color: 'yellow',
      closes: false,
    };
  }
  return migrationNotice(reply, target);
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
 */
export function passedOverLine(entry: ApiPassedOver, sourceVersion: number | undefined, t: Translate): string {
  if (!entry.cause || sourceVersion === undefined) return entry.reason;
  const key = `migration.passedOver.${entry.cause}`;
  const steps = stepsNamed(entry, t);
  // `steps` is left out rather than sent blank: `t` leaves a placeholder it
  // was given nothing for as written, which is how the hole is seen.
  const line = t(key, steps === '' ? { version: sourceVersion } : { steps, version: sourceVersion });
  if (line === key || (steps === '' && line.includes('{steps}'))) return entry.reason;
  return line;
}

/** `"Legal review", "Credit check" and 15 more`: the steps as people know them. */
function stepsNamed(entry: ApiPassedOver, t: Translate): string {
  const steps = entry.steps ?? [];
  const names = steps.map((step) => `"${step.name || step.node_id}"`).join(', ');
  const more = (entry.steps_in_all ?? steps.length) - steps.length;
  return names !== '' && more > 0 ? `${names} ${t('migration.passedOverStepsMore', { count: more })}` : names;
}

/**
 * When a request stops waiting, as the reader's language writes a date.
 *
 * Never throws: this is said of a request that has been sent, and an
 * exception on the way to saying it would be reported as an apply the server
 * did not confirm. A time that cannot be read is shown as it came, and a
 * language the platform does not know is written as English writes it.
 */
export function formatExpiry(iso: string, locale: string, timeZone?: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const options: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short', timeZone };
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

/** Everything an apply's answer gives the dialog to say. */
export interface MigrationOutcome {
  notice: MigrationNotice;
  /** A request waits on a second administrator: nothing here is left to apply. */
  waits: boolean;
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
  if (reply.pending_approval) {
    const reasons = (reply.pending_approval.because ?? []).filter((reason) => reason.trim() !== '');
    return { notice, waits: true, listTitle: t('migration.pendingWhy'), reasons, passedOver: [], more: null };
  }
  const listed = (reply.passed_over ?? []).slice(0, MAX_PASSED_OVER_SHOWN);
  const unlisted = passedOverCount(reply) - listed.length;
  return {
    notice,
    waits: false,
    listTitle: t('migration.passedOverListTitle'),
    reasons: [],
    passedOver: listed.map((entry, index) => ({
      key: `${index}:${entry.instance_id}`,
      instance: t('migration.passedOverInstance', { reference: instanceReference(entry.instance_id) }),
      why: passedOverLine(entry, reply.plan?.source_version, t),
    })),
    more: unlisted > 0 ? t('migration.passedOverMore', { count: unlisted }) : null,
  };
}

/**
 * Whether the dialog itself says this outcome, rather than a toast.
 *
 * A toast is gone in seconds and holds a sentence. Who a request waits on,
 * until when and why, and which instances were not moved and why, are for
 * reading — so the dialog stays open and shows them. Every other outcome is
 * the toast it always was.
 */
export function saidInDialog(outcome: MigrationOutcome): boolean {
  return outcome.waits || outcome.passedOver.length > 0 || outcome.more !== null;
}

/** What the dialog does once an apply has answered. */
export interface AfterApply {
  /** Keep the answer on screen, in the dialog. Otherwise it is said in a toast. */
  keep: boolean;
  /** Work the plan out again for the same request. */
  replan: boolean;
  /** Close the dialog. */
  close: boolean;
}

/**
 * An answer is kept on screen or said in a toast, never both, and only a
 * toast can close the dialog. A run that left instances behind is planned
 * again, as an apply that stopped part-way is: the plan in hand counts
 * instances the run has since dealt with, and what is left is what the next
 * apply is for. A request that waits changed nothing, so the plan that was
 * sent stays as it is.
 *
 * `open` is whether the dialog is still there to keep it in. One closed while
 * the apply was on its way has nowhere to show the answer, so it is a toast
 * after all: a request that was sent must not go unsaid.
 */
export function afterApply(outcome: MigrationOutcome, open = true): AfterApply {
  const keep = open && saidInDialog(outcome);
  return { keep, replan: keep && !outcome.waits, close: !keep && outcome.notice.closes };
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
 * is written for — until the next apply or until the dialog closes.
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
