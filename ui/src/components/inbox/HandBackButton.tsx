import { Button, Tooltip } from '@mantine/core';
import { Undo2 } from 'lucide-react';

import { useTranslation } from '../../i18n/context';

interface HandBackButtonProps {
  taskName: string;
  /** Who the task goes back to. */
  owner: string;
  /**
   * Who is working on it, when that is not the reader: an administrator
   * handing back somebody else's task is asked why first, so for them the
   * button opens a dialog rather than doing it at one press.
   */
  delegate?: string;
  onHandBack: () => void;
  /** The hand-back has been sent and not yet answered. */
  busy?: boolean;
}

/**
 * What a delegate is offered where Complete would be.
 *
 * A delegated task is not the delegate's to complete, and the server refuses
 * it. Offering Complete would only earn that refusal; this is the thing they
 * can do, and its tooltip says why it is not Complete.
 */
export function HandBackButton({ taskName, owner, delegate, onHandBack, busy = false }: HandBackButtonProps) {
  const { t } = useTranslation();
  const forSomebodyElse = delegate !== undefined;
  return (
    <Tooltip
      // On focus as well as on hover: it says why this is not Complete, and
      // somebody using the keyboard needs to be told that too.
      events={{ hover: true, focus: true, touch: true }}
      label={forSomebodyElse ? t('handover.handBackForFirst', { delegate, owner }) : t('handover.handBackFirst', { owner })}
    >
      <Button
        size="xs"
        variant="filled"
        color="indigo"
        leftSection={<Undo2 size={14} aria-hidden />}
        aria-label={t('handover.handBackLabel', { task: taskName, owner })}
        aria-haspopup={forSomebodyElse ? 'dialog' : undefined}
        onClick={onHandBack}
        loading={busy}
      >
        {t('handover.handBack')}
      </Button>
    </Tooltip>
  );
}
