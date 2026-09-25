/**
 * What an audit entry records, whichever spelling wrote it.
 *
 * Task actions used to be written twice — by the task service, which names
 * who acted (`task_claimed`), and by the audit observer from the event
 * (`TaskClaimed`). They are written once now, by the service. Entries from
 * before that keep the observer's spelling, so both have to read the same.
 */
export type TimelineKind =
  | 'started'
  | 'reached'
  | 'available'
  | 'claimed'
  | 'released'
  | 'assigned'
  | 'delegated'
  | 'completed'
  | 'withdrawn'
  | 'ended'
  | 'incident'
  | 'decision'
  | 'other';

// A Map, not an object literal: the type comes from the server, and a lookup
// on a plain object answers `constructor` with a function.
const KINDS = new Map<string, TimelineKind>([
  ['ProcessStarted', 'started'],
  ['process_started', 'started'],
  ['NodeReached', 'reached'],
  ['node_reached', 'reached'],
  ['TaskCreated', 'available'],
  ['task_created', 'available'],
  ['TaskClaimed', 'claimed'],
  ['task_claimed', 'claimed'],
  ['task_unclaimed', 'released'],
  ['task_assigned', 'assigned'],
  ['task_delegated', 'delegated'],
  ['TaskCompleted', 'completed'],
  ['task_completed', 'completed'],
  ['TaskCanceled', 'withdrawn'],
  ['ProcessCompleted', 'ended'],
  ['process_ended', 'ended'],
  ['IncidentCreated', 'incident'],
  ['incident_created', 'incident'],
  ['decision_evaluated', 'decision'],
]);

export function timelineKind(type: string): TimelineKind {
  return KINDS.get(type) ?? 'other';
}
