/**
 * "Try it" in the decision editor: which stored table it runs, with which
 * values, and which lines of the grid its answer points at.
 *
 * Try it runs what the server holds, not what is on screen, so each of those
 * answers has to be pinned to the stored table. The page used to work them out
 * inline from whatever was on screen at the time, and got each of them wrong in
 * a different way.
 */

/** A stored decision, as much of one as Try it needs to name it. */
export interface TrialTarget {
  key: string;
  version: number;
}

/**
 * The table Try it runs: the one the editor loaded, at the version it loaded.
 *
 * The page sent the key typed into the editor, with no version. An edited key
 * names another table or none, and saving does not change a stored key; with
 * no version the server answers with the newest table of that key, which need
 * not be the one on screen. A table that was never saved has nothing to run.
 */
export function trialTarget(saved: TrialTarget | undefined): TrialTarget | null {
  return saved ? { key: saved.key, version: saved.version } : null;
}
