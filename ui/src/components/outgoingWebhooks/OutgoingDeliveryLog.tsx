/**
 * What a webhook was sent, newest first, and what came of it.
 *
 * Each delivery says what happened and to which case, how its last try was
 * answered, how many tries it has had and when the next one is. Opening one
 * shows what Metis sent and what the receiver said, and the two things that
 * can be done about it: try it again now, or stop trying.
 */
import {
  Alert,
  Badge,
  Button,
  Code,
  Drawer,
  Group,
  SegmentedControl,
  Stack,
  Table,
  Text,
  UnstyledButton,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { RotateCcw, Send, Square } from 'lucide-react';
import { useState, useTransition } from 'react';

import {
  MAX_ATTEMPTS,
  deliveryActions,
  deliveryOutcome,
  deliverySubject,
  eventLabel,
  nextTryLabel,
  prettyBody,
  triesLabel,
  troubleHint,
  type DeliveryTone,
} from '../../domain/outgoingWebhooks';
import {
  useOutgoingDeliveries,
  useRetryOutgoingDelivery,
  useStopOutgoingDelivery,
} from '../../hooks/useOutgoingWebhooks';
import type {
  ApiOutgoingDelivery,
  ApiOutgoingWebhook,
  DeliveryStatus,
} from '../../services/domains/outgoingWebhookService';
import { errorMessage } from '../../services/shared/errors';

interface Props {
  hook: ApiOutgoingWebhook | null;
  onClose: () => void;
  onSendTest: (hook: ApiOutgoingWebhook) => void;
}

export function OutgoingDeliveryLog({ hook, onClose, onSendTest }: Props) {
  return (
    <Drawer
      opened={hook !== null}
      onClose={onClose}
      position="right"
      size="xl"
      title={hook ? `${hook.name} · deliveries` : ''}
    >
      {hook && <Log key={hook.id} hook={hook} onSendTest={onSendTest} />}
    </Drawer>
  );
}

const FILTERS: { value: DeliveryStatus | 'all'; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'pending', label: 'Waiting to retry' },
  { value: 'failed', label: 'Gave up' },
  { value: 'delivered', label: 'Delivered' },
];

const TONE_COLOR: Record<DeliveryTone, string> = {
  delivered: 'green',
  retrying: 'yellow',
  failed: 'red',
  test: 'gray',
};

function Log({ hook, onSendTest }: { hook: ApiOutgoingWebhook; onSendTest: (hook: ApiOutgoingWebhook) => void }) {
  const [filter, setFilter] = useState<DeliveryStatus | 'all'>('all');
  const [, startTransition] = useTransition();
  const [openId, setOpenId] = useState<string | null>(null);
  const log = useOutgoingDeliveries(hook.id, filter === 'all' ? undefined : filter);
  const deliveries = log.data?.pages.flatMap((page) => page.deliveries) ?? [];
  const open = deliveries.find((delivery) => delivery.id === openId) ?? null;

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Code fz={11}>{hook.url}</Code>
        <Button size="xs" variant="default" leftSection={<Send size={14} />} onClick={() => onSendTest(hook)}>
          Send test
        </Button>
      </Group>
      <SegmentedControl
        size="xs"
        data={FILTERS}
        value={filter}
        onChange={(value) => startTransition(() => setFilter(value as DeliveryStatus | 'all'))}
      />

      {log.isError && (
        <Alert color="red" variant="light" title="The log could not be read">
          {errorMessage(log.error)}
        </Alert>
      )}
      {log.isSuccess && deliveries.length === 0 && (
        <Text size="sm" c="dimmed">
          {filter === 'all' ? 'Nothing has been sent to this webhook yet.' : 'No deliveries stand there.'}
        </Text>
      )}
      {deliveries.length > 0 && (
        <Table verticalSpacing="xs" highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>What happened</Table.Th>
              <Table.Th>Result</Table.Th>
              <Table.Th>Tries</Table.Th>
              <Table.Th>Next</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {deliveries.map((delivery) => (
              <DeliveryRow
                key={delivery.id}
                delivery={delivery}
                selected={delivery.id === openId}
                onOpen={() => setOpenId(delivery.id === openId ? null : delivery.id)}
              />
            ))}
          </Table.Tbody>
        </Table>
      )}
      {log.hasNextPage && (
        <Button variant="subtle" size="xs" loading={log.isFetchingNextPage} onClick={() => log.fetchNextPage()}>
          Show older deliveries
        </Button>
      )}

      {open && <DeliveryDetail hookId={hook.id} delivery={open} />}

      <Text size="xs" c="dimmed">
        Metis tries again after 1 min, 5 min, 15 min and longer, {MAX_ATTEMPTS} tries over about a day, then gives up
        and keeps the delivery here. A test is never tried again.
      </Text>
    </Stack>
  );
}

export function DeliveryRow({
  delivery,
  selected,
  onOpen,
}: {
  delivery: ApiOutgoingDelivery;
  selected: boolean;
  onOpen: () => void;
}) {
  const outcome = deliveryOutcome(delivery);
  const subject = deliverySubject(delivery.body);
  const when = new Date(delivery.created_at).toLocaleString();
  return (
    <Table.Tr bg={selected ? 'var(--mantine-color-default-hover)' : undefined}>
      <Table.Td>
        <UnstyledButton onClick={onOpen} aria-expanded={selected} aria-label={`Show the delivery of ${eventLabel(delivery.event)} from ${when}`}>
          <Text size="sm" fw={500}>
            {delivery.test ? `Test · ${eventLabel(delivery.event)}` : eventLabel(delivery.event)}
          </Text>
          <Text size="xs" c="dimmed">
            {[subject, when].filter((part) => part !== '').join(' · ')}
          </Text>
        </UnstyledButton>
      </Table.Td>
      <Table.Td>
        {/* A dot, not a tint: green and yellow text on their own tint measure
            3.81:1 and 2.69:1 in this theme, under AA. The label keeps the
            page's text colour and the dot carries the tone. */}
        <Badge color={TONE_COLOR[outcome.tone]} variant="dot" styles={{ label: { textTransform: 'none' } }}>
          {outcome.label}
        </Badge>
      </Table.Td>
      <Table.Td>
        <Text size="sm">{triesLabel(delivery)}</Text>
      </Table.Td>
      <Table.Td>
        <Text size="sm">{nextTryLabel(delivery)}</Text>
      </Table.Td>
    </Table.Tr>
  );
}

function DeliveryDetail({ hookId, delivery }: { hookId: string; delivery: ApiOutgoingDelivery }) {
  const retry = useRetryOutgoingDelivery(hookId);
  const stop = useStopOutgoingDelivery(hookId);
  const actions = deliveryActions(delivery);
  const hint = delivery.status === 'delivered' ? null : troubleHint(delivery.last_status_code, delivery.last_error);

  const act = (mutation: typeof retry, done: string) =>
    mutation.mutate(delivery.id, {
      onSuccess: () => notifications.show({ message: done, color: 'green' }),
      onError: (err) => notifications.show({ title: 'Nothing was changed', message: errorMessage(err), color: 'red' }),
    });

  return (
    <Stack gap="sm" p="md" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 8 }}>
      <Text size="sm" fw={600}>
        {eventLabel(delivery.event)}
        {deliverySubject(delivery.body) !== '' && ` · ${deliverySubject(delivery.body)}`}
      </Text>
      <Text size="xs" c="dimmed">
        Delivery <Code fz={10}>{delivery.id}</Code>, the same ID on every try
      </Text>
      {delivery.last_attempt_at && (
        <Text size="sm">
          {/* The server's own words already name the status when there was one. */}
          Last tried {new Date(delivery.last_attempt_at).toLocaleString()}:{' '}
          {delivery.last_error || (delivery.last_status_code ? `answered ${delivery.last_status_code}` : 'no answer')}
        </Text>
      )}
      {hint && (
        <Alert color="yellow" variant="light">
          {hint}
        </Alert>
      )}
      {delivery.last_response && (
        <div>
          <Text size="xs" fw={600} mb={4}>
            What the receiver said
          </Text>
          <Code block fz={11}>
            {delivery.last_response}
          </Code>
        </div>
      )}
      <div>
        <Text size="xs" fw={600} mb={4}>
          What Metis sent
        </Text>
        <Code block fz={11}>
          {prettyBody(delivery.body)}
        </Code>
      </div>
      {(actions.retry || actions.stop) && (
        <Group justify="flex-end">
          {actions.stop && (
            <Button
              variant="default"
              color="red"
              size="xs"
              leftSection={<Square size={14} />}
              loading={stop.isPending}
              onClick={() => act(stop, 'Stopped. Nothing more is sent of this delivery.')}
            >
              Stop retrying
            </Button>
          )}
          {actions.retry && (
            <Button
              size="xs"
              leftSection={<RotateCcw size={14} />}
              loading={retry.isPending}
              onClick={() => act(retry, 'It is tried again now.')}
            >
              Retry now
            </Button>
          )}
        </Group>
      )}
    </Stack>
  );
}
