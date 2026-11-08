import { REASON_LIMIT } from './taskDelegation';
import type { HeldWork } from '../services/domains/participantService';

/** What deactivating somebody is asked to do with the tasks they hold. */
export interface HeldWorkChoice {
  do: HeldWork['do'];
  to: string | null;
  reason: string;
}

/**
 * Whether the deactivation can be sent. Moving somebody's work needs a reason,
 * because whoever deactivates them does not hold it, and a hand-over needs
 * somebody to hand it to. With nothing held there is nothing to decide.
 */
export function activationReady(choice: HeldWorkChoice, held: number): boolean {
  if (held === 0 || choice.do === 'keep') return true;
  const reason = choice.reason.trim();
  if (reason === '' || [...reason].length > REASON_LIMIT) return false;
  return choice.do !== 'hand_over' || !!choice.to;
}

/** The work to send with the deactivation, or nothing when nobody's tasks move. */
export function heldWorkToSend(choice: HeldWorkChoice, held: number): HeldWork | undefined {
  if (held === 0 || choice.do === 'keep') return undefined;
  const work: HeldWork = { do: choice.do, reason: choice.reason.trim() };
  if (choice.do === 'hand_over' && choice.to) work.to = choice.to;
  return work;
}
