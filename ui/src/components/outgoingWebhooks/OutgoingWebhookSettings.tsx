/**
 * The addresses this installation tells when something happens in a
 * project's processes: a ledger that wants every completed purchase, a pager
 * that wants every incident.
 *
 * Incoming webhooks (WebhookSettings) are the other half: a partner telling
 * this engine. Both sit on the connectors page because to whoever is wiring a
 * system up they are one job.
 *
 * Anyone in the project can see what is sent where. Registering one, trying
 * it and reading what receivers answered are an administrator's: a webhook is
 * a standing channel out of the project. The server refuses anybody else
 * either way; the controls are hidden so nobody is shown one that only ever
 * says no.
 */
import { ActionIcon, Badge, Button, Card, Group, Stack, Switch, Table, Text, Title, Tooltip } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { ListChecks, Plus, Send, Trash2, Webhook } from 'lucide-react';
import { useState } from 'react';

import { hasRole } from '../../domain/access';
import { eventLabel } from '../../domain/outgoingWebhooks';
import { PRIVILEGED_ROLE } from '../../domain/roles';
import {
  useDeleteOutgoingWebhook,
  useOutgoingWebhooks,
  useSetOutgoingWebhookEnabled,
} from '../../hooks/useOutgoingWebhooks';
import type { ApiOutgoingWebhook } from '../../services/domains/outgoingWebhookService';
import { errorMessage } from '../../services/shared/errors';
import { useAppStore } from '../../store/useAppStore';
import { CreateOutgoingWebhook } from './CreateOutgoingWebhook';
import { OutgoingDeliveryLog } from './OutgoingDeliveryLog';
import { SendOutgoingTest } from './SendOutgoingTest';

export function OutgoingWebhookSettings() {
  const isAdmin = useAppStore((state) => hasRole(state.user, PRIVILEGED_ROLE));
  const { data } = useOutgoingWebhooks();
  const setEnabled = useSetOutgoingWebhookEnabled();
  const remove = useDeleteOutgoingWebhook();

  const [formOpen, form] = useDisclosure(false);
  const [testing, setTesting] = useState<ApiOutgoingWebhook | null>(null);
  const [reading, setReading] = useState<ApiOutgoingWebhook | null>(null);

  const webhooks = data ?? [];

  const removeWebhook = (hook: ApiOutgoingWebhook) => {
    if (!window.confirm(`Remove the ${hook.name} webhook? Nothing more is sent to it, and what it is still owed is dropped.`)) return;
    remove.mutate(hook.id, {
      onError: (err) => notifications.show({ title: `Could not remove ${hook.name}`, message: errorMessage(err), color: 'red' }),
    });
  };

  // A test from the log opens over it; closing the test goes back to the log.
  const sendTest = (hook: ApiOutgoingWebhook) => setTesting(hook);

  return (
    <Card withBorder radius="lg" p="xl">
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <div>
            <Group gap="xs">
              <Webhook size={18} />
              <Title order={4}>Outgoing webhooks</Title>
            </Group>
            <Text size="xs" c="dimmed">
              Tell other systems when something happens in your processes. Each delivery is signed, and retried until
              the receiver accepts it.
            </Text>
          </div>
          {isAdmin && (
            <Button size="xs" leftSection={<Plus size={14} />} onClick={form.open}>
              New outgoing webhook
            </Button>
          )}
        </Group>

        {webhooks.length === 0 ? (
          <Text size="sm" c="dimmed">
            None yet. {isAdmin ? 'Add one to tell another system what happens here.' : 'An administrator can add one.'}
          </Text>
        ) : (
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Sends</Table.Th>
                <Table.Th>On</Table.Th>
                {isAdmin && <Table.Th />}
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {webhooks.map((hook) => (
                <Table.Tr key={hook.id}>
                  <Table.Td>
                    <Text size="sm" fw={500}>
                      {hook.name}
                    </Text>
                    <Text size="xs" c="dimmed" style={{ wordBreak: 'break-all' }}>
                      {hook.url}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Group gap={4}>
                      {hook.events.map((event) => (
                        <Badge key={event} variant="light" size="sm" styles={{ label: { textTransform: 'none' } }}>
                          {eventLabel(event)}
                        </Badge>
                      ))}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Switch
                      size="xs"
                      aria-label={`Switch ${hook.name} ${hook.enabled ? 'off' : 'on'}`}
                      checked={hook.enabled}
                      disabled={!isAdmin}
                      onChange={(event) =>
                        setEnabled.mutate(
                          { id: hook.id, enabled: event.currentTarget.checked },
                          {
                            onError: (err) =>
                              notifications.show({ title: 'Could not switch it', message: errorMessage(err), color: 'red' }),
                          },
                        )
                      }
                    />
                  </Table.Td>
                  {isAdmin && (
                    <Table.Td>
                      <Group gap={4} wrap="nowrap" justify="flex-end">
                        <Button size="compact-xs" variant="default" leftSection={<Send size={12} />} onClick={() => sendTest(hook)}>
                          Send test
                        </Button>
                        <Button
                          size="compact-xs"
                          variant="default"
                          leftSection={<ListChecks size={12} />}
                          onClick={() => setReading(hook)}
                        >
                          Deliveries
                        </Button>
                        <Tooltip label="Remove: nothing more is sent to it">
                          <ActionIcon
                            aria-label={`Delete the ${hook.name} webhook`}
                            variant="subtle"
                            color="red"
                            size="sm"
                            onClick={() => removeWebhook(hook)}
                          >
                            <Trash2 size={14} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  )}
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Stack>

      {isAdmin && (
        <>
          <CreateOutgoingWebhook opened={formOpen} onClose={form.close} onSendTest={sendTest} />
          <OutgoingDeliveryLog hook={reading} onClose={() => setReading(null)} onSendTest={sendTest} />
          <SendOutgoingTest hook={testing} onClose={() => setTesting(null)} />
        </>
      )}
    </Card>
  );
}
