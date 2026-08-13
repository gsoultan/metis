/**
 * What each status is called, what colour it is, and which icon stands for it.
 *
 * Apart from the badge so that a screen can borrow the wording without
 * rendering one — the kanban headings do — and because a module exporting both
 * a component and a table cannot be hot-reloaded.
 */
import {
  AlertCircle,
  CheckCircle,
  CircleDashed,
  Hand,
  Hourglass,
  Pause,
  Play,
  Square,
  UserCheck,
} from 'lucide-react';
import type { MantineColor } from '@mantine/core';
import type { ComponentType } from 'react';

export interface StatusPresentation {
  label: string;
  color: MantineColor;
  icon: ComponentType<{ size?: number | string }>;
}

export const STATUS: Record<string, StatusPresentation> = {
  // Running
  active: { label: 'Running', color: 'blue', icon: Play },
  running: { label: 'Running', color: 'blue', icon: Play },
  in_progress: { label: 'In progress', color: 'blue', icon: Play },

  // Finished well
  completed: { label: 'Completed', color: 'green', icon: CheckCircle },
  done: { label: 'Completed', color: 'green', icon: CheckCircle },
  finished: { label: 'Completed', color: 'green', icon: CheckCircle },

  // Finished badly, or needs a person
  failed: { label: 'Needs attention', color: 'red', icon: AlertCircle },
  error: { label: 'Needs attention', color: 'red', icon: AlertCircle },
  incident: { label: 'Needs attention', color: 'red', icon: AlertCircle },

  // Not started, or waiting on something
  pending: { label: 'Not started', color: 'yellow', icon: CircleDashed },
  todo: { label: 'Not started', color: 'yellow', icon: CircleDashed },
  waiting: { label: 'Waiting', color: 'orange', icon: Hourglass },

  // Work that a person picks up
  unclaimed: { label: 'Available', color: 'grape', icon: Hand },
  claimed: { label: 'Claimed', color: 'indigo', icon: UserCheck },
  delegated: { label: 'Delegated', color: 'indigo', icon: UserCheck },

  // Stopped deliberately
  suspended: { label: 'Paused', color: 'gray', icon: Pause },
  terminated: { label: 'Stopped', color: 'dark', icon: Square },
  cancelled: { label: 'Stopped', color: 'dark', icon: Square },
};

/** The plain-language label for a status, for use outside a badge. */
export function statusLabel(status: string): string {
  return STATUS[status?.toLowerCase()]?.label ?? status;
}
