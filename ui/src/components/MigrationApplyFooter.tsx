import { Alert, Button, Group, List, Loader, ScrollArea, Stack, Text } from '@mantine/core';
import { AlertTriangle, UserCheck } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import type { RefObject } from 'react';

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
 * A step's name, a reason, a reference: words this did not write, which may
 * have no place to break. They wrap where they must rather than be cut off
 * by the edge of the dialog.
 */
const WRAPS = { overflowWrap: 'anywhere' } as const;

/**
 * The foot of the migration dialog: what the last apply did, what the next
 * press would do, and the buttons.
 *
 * Every word here is decided in `domain/migrationOutcome` and
 * `domain/migrationApproval`; this only lays them out. What it does decide is
 * what is offered next, and that the press which sent a request cannot also
 * be what dismisses its confirmation.
 *
 * After a request was sent there is nothing left to apply, and the other
 * button says "Close" — "Cancel" under a request just sent reads as
 * withdrawing it, which it does not do. The apply button is not removed but
 * kept in its place, unseen and unpressable, so that "Close" stays where
 * "Cancel" was: the second click of a double click lands where the first did,
 * and must find nothing there. Focus, which the pressed button had, goes to
 * the message and not to "Close": a key still held from the press does
 * nothing on a message, and would close the dialog on a button.
 */
export function MigrationApplyFooter({ outcome, needed, label, ready, applying, planning, onApply, onClose }: MigrationApplyFooterProps) {
  const { t } = useTranslation();
  const waits = outcome?.waits === true;
  const message = useRef<HTMLDivElement>(null);
  const applyButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    // Only when focus is nowhere, or still on the button that has just been
    // hidden: never taken from a field somebody is typing in.
    const focused = document.activeElement;
    const adrift = focused === null || focused === document.body || focused === applyButton.current;
    if (waits && adrift) message.current?.focus();
  }, [waits]);
  return (
    <>
      {outcome !== null && <OutcomePanel outcome={outcome} messageRef={message} />}
      {/* Not beside a request that waits: that says the same, of what was sent. */}
      {needed !== null && !waits && <SecondApproverNotice needed={needed} />}
      <Group justify="flex-end">
        {planning && (
          <Group gap={6} mr="auto">
            <Loader size="xs" />
            <Text size="xs" c="dimmed">Working out the plan for this change…</Text>
          </Group>
        )}
        <Button variant="subtle" color="gray" onClick={onClose}>
          {outcome !== null ? t('common.close') : 'Cancel'}
        </Button>
        {waits ? (
          <Button ref={applyButton} color="orange" disabled aria-hidden tabIndex={-1} style={{ visibility: 'hidden' }}>
            {label}
          </Button>
        ) : (
          <Button ref={applyButton} color="orange" disabled={!ready} loading={applying} onClick={onApply}>
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
              <List.Item key={`${index}:${reason}`} style={WRAPS}>{reason}</List.Item>
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
 * The sentences are an alert, so they are announced as the dialog's other
 * notices are, and the alert can take focus (it is not a tab stop). The list
 * under it is outside the alert on purpose: an alert is read out whole and at
 * once, and two hundred instances read out as one interruption is not an
 * announcement. It is a list a screen reader counts, named by the line above
 * it, and it scrolls inside the dialog — by keyboard too, which is what the
 * tab stop on the scrolling region is for. The region is a tab stop only when
 * it does scroll: two lines that fit are not somewhere to stop on the way to
 * "Close".
 *
 * A request's reference is an element of its own, selected whole by one
 * click: it is what the administrator who approves is given.
 */
function OutcomePanel({ outcome, messageRef }: { outcome: MigrationOutcome; messageRef: RefObject<HTMLDivElement | null> }) {
  const titleId = useId();
  const [scrolls, setScrolls] = useState(false);
  const { notice, reference } = outcome;
  const Icon = outcome.waits ? UserCheck : AlertTriangle;
  const hasList = outcome.reasons.length > 0 || outcome.passedOver.length > 0;
  return (
    <Stack gap={6}>
      <Alert ref={messageRef} tabIndex={-1} color={notice.color} icon={<Icon size={16} />} radius="md">
        <Stack gap={2}>
          <Text size="sm" fw={600}>{notice.title}</Text>
          <Text size="sm" style={WRAPS}>{notice.message}</Text>
          {outcome.how !== null && <Text size="sm">{outcome.how}</Text>}
          {reference !== null && (
            <Text size="sm">
              {reference.before}
              <Text span ff="monospace" size="sm" style={{ ...WRAPS, userSelect: 'all' }}>{reference.value}</Text>
              {reference.after}
            </Text>
          )}
        </Stack>
      </Alert>
      {hasList && <Text id={titleId} size="xs" fw={600}>{outcome.listTitle}</Text>}
      {outcome.reasons.length > 0 && (
        <List size="xs" spacing={2} withPadding aria-labelledby={titleId}>
          {outcome.reasons.map((reason, index) => (
            <List.Item key={`${index}:${reason}`} style={WRAPS}>{reason}</List.Item>
          ))}
        </List>
      )}
      {outcome.passedOver.length > 0 && (
        <ScrollArea.Autosize
          mah={260}
          scrollbars="y"
          offsetScrollbars
          onOverflowChange={setScrolls}
          viewportProps={{ tabIndex: scrolls ? 0 : undefined, role: 'group', 'aria-labelledby': titleId }}
        >
          <List size="xs" spacing={6} withPadding>
            {outcome.passedOver.map((row) => (
              <List.Item key={row.key}>
                <Text span display="block" size="xs" fw={600}>{row.instance}</Text>
                <Text span display="block" size="xs" style={WRAPS}>{row.why}</Text>
              </List.Item>
            ))}
          </List>
        </ScrollArea.Autosize>
      )}
      {outcome.more !== null && <Text size="xs" c="dimmed">{outcome.more}</Text>}
    </Stack>
  );
}
