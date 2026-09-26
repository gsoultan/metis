/**
 * What a version change does to work that is already running.
 *
 * The engine's rule is fixed and this module only describes it: a process
 * instance pins the exact version it started on and finishes on that graph,
 * whatever is deployed afterwards. Changing the live version therefore only ever
 * redirects *new* instances. Nothing moves work between versions, because there
 * is no general way to relocate a token from one graph onto another — the node
 * it is waiting on may simply not exist in the new one.
 *
 * That makes replacing a version a drain, not a switch, and a drain has a state
 * worth naming: the previous version keeps executing, with no new arrivals,
 * until its last instance finishes.
 */

import { hasBlockingIssues, type ValidationIssue } from './processValidation';

/** One deployed version, as the versions endpoint reports it. */
export interface VersionStatus {
  version: number;
  live: boolean;
  running_instances: number;
  total_instances?: number;
}

/**
 * `live`     — takes new instances.
 * `draining` — replaced, still finishing work it started. Cannot be deleted
 *              safely and cannot be hurried.
 * `staged`   — deployed but never live, and newer than the version that is.
 *              Waiting to be promoted or scheduled.
 * `retired`  — was live once, and is finished. Kept for history and for the
 *              instances that reference it, but carrying nothing.
 */
export type VersionState = 'live' | 'draining' | 'staged' | 'retired';

/**
 * Needs the whole list, not just the row, because "staged" and "retired" are the
 * same row seen from different sides: both are not live and carrying nothing,
 * and what separates them is whether they are ahead of the live version or
 * behind it. Judging the row alone labelled every staged version "Retired" —
 * the most discouraging possible word for something waiting to go live.
 */
export function versionState(version: VersionStatus, versions: VersionStatus[]): VersionState {
  if (version.live) return 'live';
  if (version.running_instances > 0) return 'draining';
  const live = liveVersion(versions);
  if ((version.total_instances ?? 0) === 0 && live !== null && version.version > live.version) {
    return 'staged';
  }
  return 'retired';
}

/** The version new instances start on, or null when nothing is deployed. */
export function liveVersion(versions: VersionStatus[]): VersionStatus | null {
  return versions.find((v) => v.live) ?? null;
}

/**
 * Whether deploying should ask what to do, rather than just doing it.
 *
 * The choice is only real once a version exists that could stay live. The first
 * deploy of a process has no incumbent, so asking would be a question with one
 * answer.
 */
export function shouldAskRollout(versions: VersionStatus[]): boolean {
  return versions.length > 0;
}

/** The number the next deploy will claim. Versions are allocated as max + 1. */
export function nextVersionNumber(versions: VersionStatus[]): number {
  return versions.reduce((highest, v) => Math.max(highest, v.version), 0) + 1;
}

/** How many instances are still running across every version. */
export function totalRunning(versions: VersionStatus[]): number {
  return versions.reduce((sum, v) => sum + v.running_instances, 0);
}

/** Every version that has been replaced but is still finishing work. */
export function drainingVersions(versions: VersionStatus[]): VersionStatus[] {
  return versions.filter((v) => versionState(v, versions) === 'draining');
}

/**
 * Whether promoting `version` is a move backwards — a rollback.
 *
 * Worth distinguishing in the UI because it is the case where someone is
 * undoing a mistake, and the reassuring half of the message ("instances on the
 * bad version still finish on it") is also the alarming half.
 */
export function isRollback(versions: VersionStatus[], version: number): boolean {
  const live = liveVersion(versions);
  return live !== null && version < live.version;
}

/** What the confirmation needs besides the versions: the moment, and how to write one. */
export interface PromotionContext {
  now: Date;
  formatTime: (at: Date) => string;
}

/**
 * Exactly what making `target` live does, one fact per line, for the
 * confirmation that asks.
 *
 * Checked against the server rather than assumed. Promoting writes one row on
 * the release timeline, effective now, and touches no instance: every
 * instance pins the version it started on (PromoteDefinitionVersion →
 * releaseAt). New instances resolve the live version however they start — by
 * hand, from a message or a signal, or from a call activity that names no
 * version. Going forward, a cutover already scheduled for later still happens
 * when its moment comes. A rollback cancels every cutover scheduled for later,
 * since left in place the first to arrive would undo it. The dialog said the
 * first of these and only the first; the rest are the ones people act on
 * wrongly.
 */
export function promotionFacts(versions: SchedulableVersion[], target: number, context: PromotionContext): string[] {
  const live = liveVersion(versions);
  const replaced = live !== null && live.version !== target ? live : null;
  const facts = [
    `From now on, new instances start on v${target}, including ones started by a message, a signal, ` +
      'or another process that calls this one without naming a version.',
    runningFact(versions),
  ];
  // Only for a rollback: the instances on the version being left are the ones
  // somebody rolling back is worried about, and "they finish on it" is the
  // alarming half of the answer. Going forward, letting them drain is the
  // normal path, and pointing at the escape hatch would invite using it.
  if (replaced && replaced.running_instances > 0 && isRollback(versions, target)) {
    facts.push(
      `To move the ${replaced.running_instances} on v${replaced.version} onto v${target} as well, ` +
        `use "Move work" on v${replaced.version} once v${target} is live.`,
    );
  }
  if (replaced) facts.push(`v${replaced.version} is not deleted, and can be made live again from this list.`);
  const rollingBack = isRollback(versions, target);
  for (const cutover of pendingCutovers(versions)) {
    if (cutover.version === target || cutover.at.getTime() <= context.now.getTime()) continue;
    facts.push(
      rollingBack
        ? `The change to v${cutover.version} scheduled for ${context.formatTime(cutover.at)} is cancelled, ` +
            `so v${target} stays live until somebody makes another version live.`
        : `v${cutover.version} is still scheduled to take over at ${context.formatTime(cutover.at)}. ` +
            `From then, new instances start on v${cutover.version}, not v${target}. ` +
            `Cancel it under Scheduled changes to keep v${target} live.`,
    );
  }
  return facts;
}

/** Every version with work in flight, newest first, and what happens to that work: nothing. */
function runningFact(versions: VersionStatus[]): string {
  const running = versions.filter((v) => v.running_instances > 0).sort((a, b) => b.version - a.version);
  if (running.length === 0) return 'Nothing is running on any version, so no instance is affected.';
  const parts = running.map((v, index) => {
    const one = v.running_instances === 1;
    const noun = index > 0 ? '' : one ? ' instance' : ' instances';
    return `the ${v.running_instances}${noun} on v${v.version} ${one ? 'finishes' : 'finish'} on v${v.version}`;
  });
  const listed = parts.length === 1 ? parts[0] : `${parts.slice(0, -1).join(', ')}, and ${parts[parts.length - 1]}`;
  return `Nothing already running is moved, stopped or restarted: ${listed}.`;
}

/**
 * A plain-language account of what a deploy will do, for the dialog that asks.
 *
 * Kept out of the component so the wording is testable, and so the counts and
 * the sentence describing them cannot drift apart.
 */
export interface RolloutOutcome {
  /** The version this deploy creates. */
  version: number;
  /** What stays live if the new version is staged. */
  incumbent: number | null;
  /** Instances that will keep running on the incumbent either way. */
  draining: number;
}

export function rolloutOutcome(versions: VersionStatus[]): RolloutOutcome {
  const live = liveVersion(versions);
  return {
    version: nextVersionNumber(versions),
    incumbent: live?.version ?? null,
    // Only the incumbent's own instances are described, not every running
    // instance everywhere: older versions may still be draining too, and this
    // deploy changes nothing about them.
    draining: live?.running_instances ?? 0,
  };
}

/** The minimum a list row needs for its live/staged badges. */
export interface KeyedVersion {
  key: string;
  version: number;
}

/**
 * What a list row should say about a process key, given every version of it and
 * the project's live map.
 *
 * The list used to show the highest version, on the assumption that the newest
 * one is what runs. Once a version can be staged that is wrong — and wrong in
 * the direction that matters, because it would name a version no instance will
 * start on. So the row reports the live version, and mentions a staged one
 * separately rather than in its place.
 *
 * A key absent from `live` has never been promoted, which resolves to the
 * highest version — the same fallback the engine applies.
 */
export function rowVersions<T extends KeyedVersion>(
  versions: T[],
  live: Record<string, number>,
): { live: T; staged: T | null } | null {
  if (versions.length === 0) return null;

  const highest = versions.reduce((best, v) => (v.version > best.version ? v : best), versions[0]);
  const liveNumber = live[versions[0].key];
  const liveRow = versions.find((v) => v.version === liveNumber) ?? highest;

  return {
    live: liveRow,
    // Only a version *newer* than the live one is staged. Older ones are
    // history, not something waiting to be promoted.
    staged: highest.version > liveRow.version ? highest : null,
  };
}

/** A cutover somebody has arranged that has not happened yet. */
export interface PendingCutover {
  version: number;
  /** When it takes over. */
  at: Date;
  /** The timeline entry, which is what cancelling names. */
  releaseId: string;
}

/** A version row carrying its pending cutover, as the versions endpoint reports it. */
export interface SchedulableVersion extends VersionStatus {
  scheduled_for?: string;
  scheduled_release_id?: string;
}

/**
 * The cutovers arranged for a key that have not happened yet, soonest first.
 *
 * Derived from the version rows rather than fetched separately so the schedule
 * and the versions it refers to cannot disagree — they came from one read.
 */
export function pendingCutovers(versions: SchedulableVersion[]): PendingCutover[] {
  return versions
    .filter((v) => v.scheduled_for && v.scheduled_release_id)
    .map((v) => ({
      version: v.version,
      at: new Date(v.scheduled_for as string),
      releaseId: v.scheduled_release_id as string,
    }))
    .filter((c) => !Number.isNaN(c.at.getTime()))
    .sort((a, b) => a.at.getTime() - b.at.getTime());
}

/**
 * Whether a chosen time is a cutover the server will accept.
 *
 * Mirrors the server's rule rather than replacing it: a time already gone is
 * refused there too. Checking here is only so the person finds out before
 * pressing the button, not instead of the server finding out.
 */
export function isFutureCutover(at: Date | null, now: Date): boolean {
  return at !== null && !Number.isNaN(at.getTime()) && at.getTime() > now.getTime();
}

/**
 * Whether scheduling this version makes sense.
 *
 * The live version is excluded: arranging for what is already running to start
 * running is a cutover that changes nothing, and offering it invites the reading
 * that it would somehow move the instances already on it.
 */
export function canSchedule(version: SchedulableVersion): boolean {
  return !version.live;
}

/**
 * Whether a version can be removed.
 *
 * Only one nothing has ever run. Instances pin their definition by ID, so
 * deleting a version an instance references strands a running one — the engine
 * can never load the graph it is executing — and erases the record of what a
 * finished one actually ran. The server refuses either; this is so the button is
 * not offered in the first place.
 */
export function canDelete(version: SchedulableVersion): boolean {
  return !version.live && (version.total_instances ?? 0) === 0;
}

/** Whether a deploy puts the new version live or saves it beside the live one. */
export type DeployMode = 'live' | 'staged';

/**
 * What pressing Deploy does next.
 *
 * `held` when something would stop the process running, `review` when there is
 * something worth reading first, `ask` when a live version would be replaced,
 * otherwise straight to a live deploy. Accepting the warnings is asking again
 * with none of them, so it reaches the same question a clean deploy does.
 */
export type DeployStep = 'held' | 'review' | 'ask' | 'live';

export function nextDeployStep(issues: ValidationIssue[], versions: VersionStatus[]): DeployStep {
  if (hasBlockingIssues(issues)) return 'held';
  if (issues.length > 0) return 'review';
  return shouldAskRollout(versions) ? 'ask' : 'live';
}
