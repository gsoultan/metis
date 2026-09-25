import type { ApiMigrationPlan, ApiPlannedNodeAction } from '../services/types';
import { heldTasksAffected } from './instanceMigration';

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
 */

/** The server's answer to an apply that did not fail outright. */
export interface MigrationReply {
  plan?: ApiMigrationPlan;
  applied: boolean;
}

/** What to tell somebody once an apply has answered. */
export interface MigrationNotice {
  title: string;
  message: string;
  color: 'green' | 'gray' | 'yellow';
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
