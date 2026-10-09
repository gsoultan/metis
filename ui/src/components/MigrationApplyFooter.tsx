import { Alert, Button, Group, List, Loader, ScrollArea, Stack, Text } from '@mantine/core';
import { AlertTriangle, UserCheck } from 'lucide-react';
import { useEffect, useId, useRef } from 'react';

import type { ApprovalNeeded } from '../domain/migrationApproval';
import type { MigrationOutcome } from '../domain/migrationOutcome';
import { useTranslation } from '../i18n/context';

interface MigrationApplyFooterProps {
  /** What the last apply did, while that is true of what is on screen. */
  outcome: MigrationOutcome | null;
  /** Set when applying the plan on screen would send it to somebody else. */
  needed: ApprovalNeeded | null;
  /** What the apply button says pressing it does. */
  label: string;
  /** Whether the plan on screen can be applied. */
  ready: boolean;
  applying: boolean;
  /** A plan for the latest edit is being worked out. */
  planning: boolean;
  onApply: () => void;
  onClose: () => void;
}

/**
 * The foot of the migration dialog: what the last apply did, what the next
 * press would do, and the buttons.
 *
 * Every word here is decided in `domain/migrationOutcome` and
 * `domain/migrationApproval`; this only lays them out. What it does decide is
 * what is offered next. After a request was sent there is nothing left to
 * apply, so the apply button goes — and the other one says "Close", because
 * "Cancel" under a request just sent reads as withdrawing it, which it does
 * not do.
 */
export function MigrationApplyFooter({ outcome, needed, label, ready, applying, planning, onApply, onClose }: MigrationApplyFooterProps) {
  const { t } = useTranslation();
  const waits = outcome?.waits === true;
  // The button that was pressed goes when a request waits, and focus goes
  // with it: out of the dialog, for somebody using a keyboard. It is put on
  // the one button that is left — and only when it is nowhere, so that it is
  // never taken from a field somebody is typing in.
  const closeButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const focused = document.activeElement;
    if (waits && (focused === null || focused === document.body)) closeButton.current?.focus();
  }, [waits]);
  return (
    <>
      {outcome !== null && <OutcomePanel outcome={outcome} />}
      {/* Not beside a request that waits: that says the same, of what was sent. */}
      {needed !== null && !waits && <SecondApproverNotice needed={needed} />}
      <Group justify="flex-end">
        {planning && (
          <Group gap={6} mr="auto">
            <Loader size="xs" />
            <Text size="xs" c="dimmed">Working out the plan for this change…</Text>
          </Group>
        )}
        <Button ref={closeButton} variant="subtle" color="gray" onClick={onClose}>
          {outcome !== null ? t('common.close') : 'Cancel'}
        </Button>
        {!waits && (
          <Button color="orange" disabled={!ready} loading={applying} onClick={onApply}>
            {label}
          </Button>
        )}
      </Group>
    </>
  );
}

/**
 * Said before the press. An alert, as the plan's refusals and warnings above
 * it are: it changes what the button does, and arrives with the plan.
 */
function SecondApproverNotice({ needed }: { needed: ApprovalNeeded }) {
  return (
    <Alert color="blue" icon={<UserCheck size={16} />} radius="md">
      <Stack gap={6}>
        <Text size="sm" fw={600}>{needed.title}</Text>
        <Text size="sm">{needed.message}</Text>
        {needed.reasons.length > 0 && (
          <List size="xs" spacing={2} withPadding>
            {needed.reasons.map((reason, index) => (
              <List.Item key={`${index}:${reason}`}>{reason}</List.Item>
            ))}
          </List>
        )}
      </Stack>
    </Alert>
  );
}

/**
 * What an apply did, when that is more than a toast can hold.
 *
 * The sentence is an alert, so it is announced as the dialog's other notices
 * are. The list under it is outside the alert on purpose: an alert is read
 * out whole and at once, and two hundred instances read out as one
 * interruption is not an announcement. It is a list a screen reader counts,
 * named by the line above it, and it scrolls inside the dialog — by keyboard
 * too, which is what the tab stop on the scrolling region is for.
 */
function OutcomePanel({ outcome }: { outcome: MigrationOutcome }) {
  const titleId = useId();
  const { notice } = outcome;
  const Icon = outcome.waits ? UserCheck : AlertTriangle;
  const listed = outcome.reasons.length > 0 || outcome.passedOver.length > 0;
  return (
    <Stack gap={6}>
      <Alert color={notice.color} icon={<Icon size={16} />} radius="md">
        <Stack gap={2}>
          <Text size="sm" fw={600}>{notice.title}</Text>
          <Text size="sm">{notice.message}</Text>
        </Stack>
      </Alert>
      {listed && <Text id={titleId} size="xs" fw={600}>{outcome.listTitle}</Text>}
      {outcome.reasons.length > 0 && (
        <List size="xs" spacing={2} withPadding aria-labelledby={titleId}>
          {outcome.reasons.map((reason, index) => (
            <List.Item key={`${index}:${reason}`}>{reason}</List.Item>
          ))}
        </List>
      )}
      {outcome.passedOver.length > 0 && (
        <ScrollArea.Autosize
          mah={260}
          scrollbars="y"
          offsetScrollbars
          viewportProps={{ tabIndex: 0, role: 'group', 'aria-labelledby': titleId }}
        >
          <List size="xs" spacing={6} withPadding>
            {outcome.passedOver.map((row) => (
              <List.Item key={row.key}>
                <Text span display="block" size="xs" fw={600}>{row.instance}</Text>
                <Text span display="block" size="xs">{row.why}</Text>
              </List.Item>
            ))}
          </List>
        </ScrollArea.Autosize>
      )}
      {outcome.more !== null && <Text size="xs" c="dimmed">{outcome.more}</Text>}
    </Stack>
  );
}
