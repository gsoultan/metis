import { sentences, type Translate } from './migrationOutcome';
import type { ApiMigrationPlan } from '../services/types';

/**
 * A migration that one administrator may not apply alone.
 *
 * A plan that skips a step, drops a control or loosens a rule is not made on
 * one person's say: applying it stores a request, and a different
 * administrator approves or rejects it. The server says which plans those are
 * (`requires_second_approver`) and why, in sentences. What is decided here is
 * only how the dialog says so before anything is sent — a button that read
 * "Move 3 instances" over a press that moves none was the dialog promising
 * what the server would not do.
 */

/** What to say, before the press, of a plan that will be sent to somebody else. */
export interface ApprovalNeeded {
  title: string;
  message: string;
  /** The server's reasons, as it worded them: to show, not to read apart. */
  reasons: string[];
}

/**
 * Null for a plan one administrator can apply, and for no plan.
 *
 * The flag alone decides. Reasons without it ask nobody, and a server older
 * than the second approver sends neither.
 */
export function approvalNeeded(plan: ApiMigrationPlan | null, t: Translate): ApprovalNeeded | null {
  if (plan === null || plan.requires_second_approver !== true) return null;
  return {
    title: t('migration.secondApproverTitle'),
    message: t('migration.secondApproverMessage'),
    reasons: sentences(plan.second_approver_reasons),
  };
}

/**
 * What the apply button says pressing it does.
 *
 * "Send for approval" when that is what the press does. Otherwise the count
 * it always gave, in the English the rest of the dialog is still written in.
 */
export function applyLabel(plan: ApiMigrationPlan | null, t: Translate): string {
  if (plan?.requires_second_approver === true) return t('migration.sendForApproval');
  const instances = plan?.instances ?? 0;
  return `Move ${instances} ${instances === 1 ? 'instance' : 'instances'}`;
}
