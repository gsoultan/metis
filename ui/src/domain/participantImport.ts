/**
 * Importing a directory of participants, from a file, an API or a query.
 *
 * The three sources differ only in where the rows come from. What a usable
 * participant is, and what to do about the rows that are not, is decided once
 * on the server — so this is about presenting the outcome, not re-deciding it.
 */

/** Where a directory is being read from. */
export const SOURCE_KINDS = ['csv', 'http', 'postgres'] as const;
export type SourceKind = (typeof SOURCE_KINDS)[number];

export const SOURCE_LABELS: Record<SourceKind, string> = {
  csv: 'Upload a file',
  http: 'API endpoint',
  postgres: 'Database query',
};

/** One row that was not imported, and why. */
export interface ImportProblem {
  line: number;
  username?: string;
  reason: string;
}

/** What an import did. */
export interface ImportSummary {
  created: number;
  updated: number;
  groups: number;
  problems?: ImportProblem[];
}

export interface HTTPSourceDraft {
  url: string;
  method: string;
  headers: Array<{ name: string; value: string }>;
}

export interface PostgresSourceDraft {
  dsn: string;
  query: string;
}

/**
 * Whether a source has enough to be read.
 *
 * Checked here as well as on the server, for the usual reason: the server's
 * check is the one that counts, and this one is what stops somebody pressing a
 * button that was always going to be refused.
 */
export function canRunHTTP(draft: HTTPSourceDraft): boolean {
  return draft.url.trim() !== '';
}

export function canRunPostgres(draft: PostgresSourceDraft): boolean {
  return draft.dsn.trim() !== '' && draft.query.trim() !== '';
}

/**
 * How an import went, in one line.
 *
 * Created and updated are kept apart because they answer different questions:
 * one says how many people are new, the other how many rows overwrote somebody
 * already there — which is the number worth checking when a file was meant to
 * be additive.
 */
export function summarise(summary: ImportSummary): string {
  const parts: string[] = [];
  if (summary.created > 0) parts.push(`${summary.created} added`);
  if (summary.updated > 0) parts.push(`${summary.updated} updated`);
  if (summary.groups > 0) parts.push(`${summary.groups} ${summary.groups === 1 ? 'team' : 'teams'} created`);
  if (parts.length === 0) parts.push('nothing changed');
  return parts.join(', ');
}

/**
 * Whether the outcome deserves attention rather than a green tick.
 *
 * An import that reported problems succeeded — the good rows landed — but
 * saying only "imported" would hide the rows that did not. That is the failure
 * mode this whole design exists to avoid.
 */
export function needsReview(summary: ImportSummary): boolean {
  return (summary.problems?.length ?? 0) > 0;
}

/** The tone the result should be shown in. */
export function outcomeTone(summary: ImportSummary): 'success' | 'partial' | 'none' {
  if (needsReview(summary)) return 'partial';
  if (summary.created === 0 && summary.updated === 0) return 'none';
  return 'success';
}

/**
 * Problems in the order somebody reads their file.
 *
 * A copy rather than a sort in place: the summary belongs to the query cache,
 * and reordering it there would mutate what other views are rendering from.
 */
export function problemsInFileOrder(summary: ImportSummary): ImportProblem[] {
  return [...(summary.problems ?? [])].sort((a, b) => a.line - b.line);
}

/** A participant, as the API returns them. */
export interface Participant {
  id: string;
  username: string;
  display_name?: string;
  email?: string;
  active: boolean;
  has_credentials: boolean;
  /** The teams this person is in, which is what a task's candidate groups name. */
  groups?: string[];
}

/**
 * What to show about somebody's standing.
 *
 * Three states, not two: somebody can be active and still unable to sign in,
 * because a directory import carries names rather than passwords. Collapsing
 * that into "active" would leave whoever imported five hundred people wondering
 * why none of them can log in.
 */
export type ParticipantStanding = 'ready' | 'no-credentials' | 'inactive';

export function standingOf(participant: Participant): ParticipantStanding {
  if (!participant.active) return 'inactive';
  return participant.has_credentials ? 'ready' : 'no-credentials';
}

export const STANDING_LABELS: Record<ParticipantStanding, { label: string; hint: string }> = {
  ready: { label: 'Active', hint: 'Can be assigned work. Has a password set.' },
  'no-credentials': {
    label: 'No password yet',
    hint: 'Can be assigned work. Imported without a password; an administrator can set one.',
  },
  inactive: { label: 'Inactive', hint: 'Receives no new work. Their history is kept.' },
};

/** A directory a project keeps its participants in step with. */
export interface ParticipantSource {
  id: string;
  name: string;
  /** http or postgres. A file upload is not a source — it happens once. */
  kind: Exclude<SourceKind, 'csv'>;
  config?: Record<string, unknown>;
  /** ISO 8601 repeating interval — "R/PT1H". Empty means manual only. */
  schedule?: string;
  on_missing: string;
  enabled: boolean;
  last_run?: {
    at: string;
    ok: boolean;
    detail?: string;
    created: number;
    updated: number;
  };
}

/** What a source does about somebody it has stopped naming. */
export const ON_MISSING = ['leave', 'deactivate'] as const;

export const ON_MISSING_LABELS: Record<string, { label: string; hint: string }> = {
  leave: {
    label: 'Leave them',
    hint: 'Somebody the source stops naming keeps their access. Right for a feed that covers part of the organisation.',
  },
  deactivate: {
    label: 'Stand them down',
    hint: 'Somebody the source stops naming receives no new work. Their history is kept. Only for a directory that is genuinely the source of truth.',
  },
};

/**
 * The schedules offered, as the intervals BPMN timers already use here.
 *
 * A short list rather than a free-text field: an ISO 8601 interval is easy to
 * get subtly wrong, and every value somebody actually wants is in it. The API
 * still accepts any valid interval, so this constrains the form, not the system.
 */
export const SCHEDULE_OPTIONS: Array<{ value: string; label: string }> = [
  { value: '', label: 'Only when I ask' },
  { value: 'R/PT15M', label: 'Every 15 minutes' },
  { value: 'R/PT1H', label: 'Hourly' },
  { value: 'R/PT6H', label: 'Every 6 hours' },
  { value: 'R/P1D', label: 'Daily' },
];

export function scheduleLabel(schedule: string | undefined): string {
  const found = SCHEDULE_OPTIONS.find((option) => option.value === (schedule ?? ''));
  return found ? found.label : (schedule as string);
}

/**
 * How a source is doing, in one word.
 *
 * `failing` is its own state rather than folded into `never`, because a source
 * that ran and broke needs attention and one that has not run yet does not. A
 * sync quietly failing for a month is exactly what nobody notices.
 */
export type SourceHealth = 'ok' | 'failing' | 'never' | 'disabled';

export function sourceHealth(source: ParticipantSource): SourceHealth {
  if (!source.enabled) return 'disabled';
  if (!source.last_run) return 'never';
  return source.last_run.ok ? 'ok' : 'failing';
}

export const SOURCE_HEALTH_LABELS: Record<SourceHealth, { label: string; colour: string }> = {
  ok: { label: 'Healthy', colour: 'green' },
  failing: { label: 'Failing', colour: 'red' },
  never: { label: 'Not run yet', colour: 'gray' },
  disabled: { label: 'Paused', colour: 'gray' },
};
