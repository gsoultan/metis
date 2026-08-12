import { Badge, type MantineColor } from '@mantine/core';

interface StatusBadgeProps {
  status: string;
}

/**
 * Status, in words rather than enum values.
 *
 * This rendered `status.toUpperCase()` — "ACTIVE", "FAILED", "UNCLAIMED" —
 * which is the database value shouted at the user. The wording below says what
 * the state means for the person reading it: an instance is not "ACTIVE", it is
 * "Running"; a task is not "UNCLAIMED", it is "Available".
 */
const PLAIN_STATUS: Record<string, string> = {
  active: 'Running',
  running: 'Running',
  in_progress: 'In progress',
  completed: 'Completed',
  done: 'Completed',
  finished: 'Completed',
  failed: 'Needs attention',
  error: 'Needs attention',
  pending: 'Not started',
  todo: 'Not started',
  waiting: 'Waiting',
  unclaimed: 'Available',
  claimed: 'Claimed',
  delegated: 'Delegated',
  suspended: 'Paused',
  terminated: 'Stopped',
};

export function StatusBadge({ status }: StatusBadgeProps) {
  let color: MantineColor = 'gray';
  const key = status.toLowerCase();
  const label = PLAIN_STATUS[key] ?? status;

  switch (status.toLowerCase()) {
    case 'running':
    case 'active':
    case 'in_progress':
      color = 'blue';
      break;
    case 'completed':
    case 'done':
    case 'finished':
      color = 'green';
      break;
    case 'failed':
    case 'error':
      color = 'red';
      break;
    case 'pending':
    case 'todo':
      color = 'yellow';
      break;
    case 'waiting':
      color = 'orange';
      break;
  }

  return (
    <Badge 
      variant="light" 
      color={color} 
      radius="sm" 
      style={{
        height: 26,
        minWidth: 92,
        fontWeight: 500,
        fontSize: 'var(--mantine-font-size-xs)',
      }}
    >
      {label}
    </Badge>
  );
}
