/**
 * Which version of a decision is in force, and what the others are.
 *
 * Every save of a decision is a new version, and none is ever changed. One of
 * them is live — what a process step with no version reads — and a newer one
 * can wait beside it, staged, until somebody makes it live. The server says
 * which; these helpers only put it into words for a list row.
 */

/** As much of a decision list row as its version badges need. */
export interface DecisionRowSource {
  /** The version in force; 0 or absent when none is. */
  live_version?: number;
  /** The highest stored version. */
  newest_version?: number;
}

export interface DecisionRowVersions {
  /** The version in force, or null when none is. */
  live: number | null;
  /** The newest version when it is not the live one: saved and waiting. */
  staged: number | null;
}

/**
 * The version badges a decision list row shows.
 *
 * A key with nothing live still lists its newest version as staged: it is
 * saved and reads nothing, which is the same thing a staged version is, and
 * saying "nothing live" without naming what could be made live would leave
 * the reader nowhere to go.
 */
export function decisionRowVersions(row: DecisionRowSource): DecisionRowVersions {
  const live = row.live_version && row.live_version > 0 ? row.live_version : null;
  const newest = row.newest_version ?? 0;
  const staged = newest > 0 && (live === null || newest > live) ? newest : null;
  return { live, staged };
}

/** One stored version, as the versions endpoint reports it. */
export interface DecisionVersionEntry {
  version: number;
  live: boolean;
}

/**
 * `live`    — what a step that names no version reads.
 * `staged`  — saved after the live version and never put into force: waiting.
 * `earlier` — older than the live version: history, and a rollback target.
 *
 * With nothing live, every version is staged: saved, and not in force.
 */
export type DecisionVersionState = 'live' | 'staged' | 'earlier';

/** The version in force, or null when none is. */
export function liveDecisionVersion<T extends DecisionVersionEntry>(versions: T[]): T | null {
  return versions.find((v) => v.live) ?? null;
}

export function decisionVersionState(entry: DecisionVersionEntry, versions: DecisionVersionEntry[]): DecisionVersionState {
  if (entry.live) return 'live';
  const live = liveDecisionVersion(versions);
  return live === null || entry.version > live.version ? 'staged' : 'earlier';
}

/** The number the next save will claim: one past the highest stored, not past the live one. */
export function nextDecisionVersion(versions: DecisionVersionEntry[]): number {
  return versions.reduce((highest, v) => Math.max(highest, v.version), 0) + 1;
}

/** Whether making `target` live moves backwards, to a version older than the live one. */
export function isDecisionRollback(versions: DecisionVersionEntry[], target: number): boolean {
  const live = liveDecisionVersion(versions);
  return live !== null && target < live.version;
}

/**
 * Whether a version can be deleted from the history.
 *
 * Not the live one while others remain: every step that names no version would
 * be left with nothing to read, and the server refuses it. The only version
 * can go, which deletes the decision.
 */
export function canDeleteDecisionVersion(entry: DecisionVersionEntry, versions: DecisionVersionEntry[]): boolean {
  return !entry.live || versions.length === 1;
}

/**
 * Exactly what making `target` live does, one fact per line, for the
 * confirmation that asks.
 *
 * Checked against the server rather than assumed. Making a version live writes
 * one entry on the decision's release timeline and copies nothing
 * (PromoteDecisionVersion). A decision is read when a step reaches it, not when
 * an instance starts — so, unlike a process version, it changes what running
 * instances decide from then on. What they decided already is recorded on their
 * timelines and stays as it was. A step pinned to a version reads that version
 * whatever is live.
 */
export function decisionPromotionFacts(versions: DecisionVersionEntry[], target: number): string[] {
  const live = liveDecisionVersion(versions);
  const facts = [
    `From now on, steps that name no version use v${target}, in instances already running as well, the next time they reach one.`,
    'What instances have already decided is not revisited: each keeps the answer it got and the version that gave it.',
    'Steps pinned to a version keep using the version they name.',
  ];
  if (live !== null && live.version !== target) {
    facts.push(`v${live.version} is kept, and can be made live again from this list.`);
  }
  return facts;
}

/** What saving an edit did, as the editor's notification says it. */
export interface SaveNotice {
  title: string;
  message: string;
  color: 'green' | 'blue' | 'gray';
}

export function saveNotice(
  saved: { version: number; newVersion: boolean; live: boolean },
  name: string,
  liveBefore: number | null,
): SaveNotice {
  if (!saved.newVersion) {
    return {
      title: 'Nothing to save',
      message: `${name} is the same as v${saved.version}, so no new version was made.`,
      color: 'gray',
    };
  }
  if (saved.live) {
    const kept = liveBefore !== null && liveBefore !== saved.version ? ` v${liveBefore} is kept in its history.` : '';
    return {
      title: `v${saved.version} is live`,
      message: `${name} v${saved.version} is saved and in force: steps that name no version use it from now on.${kept}`,
      color: 'green',
    };
  }
  return {
    title: `Saved as v${saved.version}, staged`,
    message: `${name} v${saved.version} is saved but not in use${liveBefore !== null ? `: v${liveBefore} stays live` : ''}. Make it live from Versions when it is ready.`,
    color: 'blue',
  };
}

/** How each state is shown: a word, a colour, and what it means for a step. */
export const DECISION_VERSION_STATES: Record<DecisionVersionState, { label: string; color: string; hint: string }> = {
  live: { label: 'Live', color: 'green', hint: 'Steps that name no version use this one.' },
  staged: { label: 'Staged', color: 'blue', hint: 'Saved and not in use yet. Make it live when it is ready.' },
  earlier: {
    label: 'Earlier',
    color: 'gray',
    hint: 'Saved before the live version. Make it live again to roll back to it.',
  },
};
