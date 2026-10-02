import { Textarea } from '@mantine/core';

import { REASON_LIMIT } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';

interface ReasonFieldProps {
  value: string;
  onChange: (value: string) => void;
  /**
   * The reason may be left out: an administrator reassigning their own task
   * needs one only when it goes to somebody it was not offered to.
   */
  optional?: boolean;
  /**
   * Put the cursor here when the dialog opens: for a dialog that asks nothing
   * else, so that somebody using the keyboard can start typing at once.
   */
  focusOnOpen?: boolean;
}

/**
 * Where somebody changing a task says why.
 *
 * The server refuses the change without it and keeps it with the task's
 * history, which is what the hint under the label says. The field stops at
 * the longest reason the server keeps, and says how much of that is used:
 * a long paste is cut short, and without the count nothing would show it.
 */
export function ReasonField({ value, onChange, optional = false, focusOnOpen = false }: ReasonFieldProps) {
  const { t } = useTranslation();
  const hint = t(optional ? 'handover.reasonHintOptional' : 'handover.reasonHint');
  const count = t('handover.reasonCount', { count: value.length, limit: REASON_LIMIT });
  return (
    <Textarea
      label={t('handover.reason')}
      description={`${hint} ${count}`}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      maxLength={REASON_LIMIT}
      required={!optional}
      autosize
      minRows={2}
      maxRows={6}
      // What the dialog's focus trap looks for when it opens.
      data-autofocus={focusOnOpen ? true : undefined}
    />
  );
}
