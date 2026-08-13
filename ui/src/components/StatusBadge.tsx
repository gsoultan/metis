import { Badge } from '@mantine/core';

import { STATUS } from './statusVocabulary';

interface StatusBadgeProps {
  status: string;
  /** Show the status icon. Useful in a dense table, noise in a sentence. */
  withIcon?: boolean;
  size?: 'xs' | 'sm' | 'md';
}

/**
 * How a status looks and reads, in one place.
 *
 * It rendered `status.toUpperCase()` — "ACTIVE", "FAILED", "UNCLAIMED" — which
 * is the database value shouted at the reader. The wording below says what the
 * state means to them: an instance is not "ACTIVE", it is "Running"; a task is
 * not "UNCLAIMED", it is "Available".
 *
 * Pages had drifted off it: the instance list wrote its own badge saying
 * "Active" where this says "Running", so the same status read differently
 * depending on the screen — which is the confusion the plain wording exists to
 * remove. And the colours covered fewer states than the words did, so a claimed
 * task and a stopped instance were both plain grey.
 */

export function StatusBadge({ status, withIcon = false, size = 'sm' }: StatusBadgeProps) {
  const presentation = STATUS[status?.toLowerCase()];
  const Icon = presentation?.icon;

  return (
    <Badge
      variant="light"
      color={presentation?.color ?? 'gray'}
      radius="sm"
      size={size}
      leftSection={withIcon && Icon ? <Icon size={11} /> : undefined}
      style={{
        height: 26,
        minWidth: 92,
        fontWeight: 500,
        fontSize: 'var(--mantine-font-size-xs)',
      }}
    >
      {presentation?.label ?? status}
    </Badge>
  );
}
