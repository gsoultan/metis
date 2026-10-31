/**
 * Releases: a project's tested process and decision versions, moved from one
 * environment to the next together.
 *
 * Cut where they were tested, imported into the next environment staged
 * (stored and checked, not live), then made live there in one step. Which of
 * those a row offers is decided here, from its state and the runtime it is
 * shown in, so the page and its tests agree.
 */

export type ReleaseState = 'cut' | 'staged' | 'live' | 'superseded';

export interface ReleaseItem {
  kind: 'process' | 'decision';
  key: string;
  /** The version here. 0: new here, stored when the release goes live. */
  version: number;
  source_version: number;
}

export interface Release {
  id: string;
  project_id: string;
  name: string;
  hash: string;
  origin: 'cut' | 'imported';
  state: ReleaseState;
  items: ReleaseItem[];
  connections: string[];
  cut_at: string;
  cut_by: string;
  acted_by: string;
  imported_by?: string;
  approved_by?: string;
  live_at?: string;
  previous?: string;
  created_at: string;
}

/** A runtime by name. An empty id is the installation's own. */
export interface RuntimeSummary {
  id: string;
  name: string;
  protected: boolean;
}

export interface RuntimeOverview {
  current: RuntimeSummary;
  environments: RuntimeSummary[];
}

/** The installation's own runtime, named so the import dialog can offer it. */
export const OWN_RUNTIME_ID = '00000000-0000-0000-0000-000000000000';

export interface ReleaseActions {
  approve: boolean;
  goLive: boolean;
}

/**
 * What a row offers. A release cut here is what already runs here: it goes
 * to the next environment, never live where it was cut. A staged one goes
 * live, and in a protected runtime is approved by a second person first.
 */
export function releaseActions(release: Release, protectedRuntime: boolean): ReleaseActions {
  const imported = release.origin === 'imported';
  const waiting = imported && (release.state === 'staged' || release.state === 'superseded');
  const approved = Boolean(release.approved_by);
  return {
    approve: protectedRuntime && release.state === 'staged' && imported && !approved,
    goLive: waiting && (!protectedRuntime || approved || release.state === 'superseded'),
  };
}

/** The badge colour of a state. */
export function releaseStateColor(state: ReleaseState): string {
  switch (state) {
    case 'live':
      return 'green';
    case 'staged':
      return 'yellow';
    case 'superseded':
      return 'gray';
    default:
      return 'blue';
  }
}

/** Whether the project has a live release here that has one before it to return to. */
export function canRollBack(releases: Release[]): boolean {
  return releases.some((r) => r.state === 'live' && Boolean(r.previous));
}

/** Process keys typed as a list: "claims, onboarding" → ["claims", "onboarding"]. */
export function parseKeys(text: string): string[] {
  return [...new Set(text.split(/[\s,]+/).map((k) => k.trim()).filter(Boolean))];
}

/** Where a runtime is, in words: its name, or "this installation". */
export function runtimeLabel(runtime: RuntimeSummary, own: string): string {
  return runtime.name || own;
}
