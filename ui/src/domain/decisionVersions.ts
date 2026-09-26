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
