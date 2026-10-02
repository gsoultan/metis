import { Text } from '@mantine/core';

import { delegatedBy, type DelegableTask, type Offer } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';
import { HandBackButton } from './HandBackButton';

interface DelegateActionProps {
  /** A task that is with a delegate (domain/taskDelegation.ts, awaitsHandBack). */
  task: DelegableTask & { name: string };
  /** How Hand back is offered to the reader (handBackOffer). */
  offer: Offer;
  onHandBack: () => void;
  /** The hand-back has been sent and not yet answered. */
  busy?: boolean;
}

/**
 * What a task that is with a delegate offers its reader where Complete would
 * be — in the table's row and on the board's card, which is why it is one
 * component and not the same branch written in each.
 *
 * It is the delegate's to work on and hand back, and its owner's to complete.
 * Release and Complete would both be refused, so neither is offered to
 * anybody: the delegate, or an administrator, is offered Hand back, and
 * everybody else is told who has it.
 */
export function DelegateAction({ task, offer, onHandBack, busy = false }: DelegateActionProps) {
  const { t } = useTranslation();
  const delegate = task.assignee?.username ?? '';
  if (offer === 'none') {
    // Always true of a task with a delegate; asked so that "With" never
    // stands there with no name after it.
    return delegate ? (
      <Text size="xs" c="dimmed">
        {t('handover.withDelegate', { delegate })}
      </Text>
    ) : null;
  }
  return (
    <HandBackButton
      taskName={task.name}
      owner={delegatedBy(task)}
      // An administrator handing back somebody else's is asked why first.
      delegate={offer === 'withReason' ? delegate : undefined}
      onHandBack={onHandBack}
      busy={busy}
    />
  );
}
