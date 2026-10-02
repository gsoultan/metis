import { Textarea } from '@mantine/core';

import { REASON_LIMIT } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';

interface ReasonFieldProps {
  value: string;
  onChange: (value: string) => void;
  /**
   * Put the cursor here when the dialog opens: for a dialog that asks nothing
   * else, so that somebody using the keyboard can start typing at once.
   */
  focusOnOpen?: boolean;
}

/**
 * Where somebody changing a task they do not hold says why.
 *
 * The server refuses the change without it and keeps it with the task's
 * history, which is what the hint under the label says. It is shown only to
 * somebody who has to fill it in, so it is always required.
 */
export function ReasonField({ value, onChange, focusOnOpen = false }: ReasonFieldProps) {
  const { t } = useTranslation();
  return (
    <Textarea
      label={t('handover.reason')}
      description={t('handover.reasonHint')}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      maxLength={REASON_LIMIT}
      required
      autosize
      minRows={2}
      maxRows={6}
      // What the dialog's focus trap looks for when it opens.
      data-autofocus={focusOnOpen ? true : undefined}
    />
  );
}
