/**
 * The addresses partners post events to.
 *
 * Connectors are the outbound half of an integration — this engine calling
 * someone. Webhooks are the inbound half: someone calling this engine when
 * something happened at their end. They sit on the same page because to whoever
 * is wiring a system up they are one job.
 *
 * The screen exists mostly to hand over two strings correctly. The token goes in
 * the partner's URL field and the secret in their signing-secret field, and the
 * secret is shown once and never again — so it is presented at the moment it is
 * created, prominently, with the warning attached rather than in a tooltip.
 *
 * It also says which webhooks still accept legacy signatures, which a captured
 * delivery can be replayed under, and until when — see LegacySigningNotice.
 */
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Code,
  CopyButton,
  Group,
  Modal,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { AlertTriangle, Check, Copy, Plus, Trash2, Webhook } from 'lucide-react';
import { useState } from 'react';

import { useCreateWebhook, useDeleteWebhook, useSetWebhookEnabled, useWebhooks } from '../hooks/useWebhooks';
import type { ApiWebhook } from '../services/domains/webhookService';
import { errorMessage } from '../services/shared/errors';
import { LegacySigningNotice } from './webhooks/LegacySigningNotice';
import { WebhookSigningHelp } from './webhooks/WebhookSigningHelp';

export function WebhookSettings() {
  const { data } = useWebhooks();
  const create = useCreateWebhook();
  const setEnabled = useSetWebhookEnabled();
  const remove = useDeleteWebhook();

  const [formOpen, form] = useDisclosure(false);
  const [name, setName] = useState('');
  const [messageName, setMessageName] = useState('');
  const [correlation, setCorrelation] = useState('');

  // Held only until the modal is dismissed. There is nowhere to fetch it from
  // afterwards, which is the point.
  const [justCreated, setJustCreated] = useState<ApiWebhook | null>(null);

  const webhooks = data?.webhooks ?? [];

  const removeWebhook = (hook: ApiWebhook) => {
    const who = hook.name || hook.message_name;
    if (!window.confirm(`Remove the ${who} webhook? The partner's URL stops working and their deliveries are refused.`)) return;
    remove.mutate(hook.id, {
      onError: (err) => notifications.show({ title: `Could not remove ${who}`, message: errorMessage(err), color: 'red' }),
    });
  };

  const submit = async () => {
    try {
      const created = await create.mutateAsync({
        name,
        message_name: messageName,
        correlation_expression: correlation || undefined,
      });
      // A refusal used to arrive here as `undefined`: the form closed, nothing
      // was said, and the secret that is shown exactly once was never shown.
      // The form stays open until there is a webhook to hand over.
      if (!created) throw new Error('The server did not return the webhook.');
      form.close();
      setName('');
      setMessageName('');
      setCorrelation('');
      setJustCreated(created);
    } catch (err: unknown) {
      notifications.show({
        title: 'Could not create the webhook',
        message: errorMessage(err, 'Something went wrong.'),
        color: 'red',
      });
    }
  };

  return (
    <Card withBorder radius="lg" p="xl">
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <div>
            <Group gap="xs">
              <Webhook size={18} />
              <Title order={4}>Incoming webhooks</Title>
            </Group>
            <Text size="xs" c="dimmed">
              Addresses a partner posts to when something happens at their end. Each delivery becomes a message your
              processes can wait for.
            </Text>
          </div>
          <Button size="xs" leftSection={<Plus size={14} />} onClick={form.open}>
            Add a webhook
          </Button>
        </Group>

        {webhooks.length === 0 ? (
          <Text size="sm" c="dimmed">
            None yet. Add one to give a partner somewhere to post events.
          </Text>
        ) : (
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Becomes the message</Table.Th>
                <Table.Th>Address</Table.Th>
                <Table.Th>On</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {webhooks.map((hook) => (
                <Table.Tr key={hook.id}>
                  <Table.Td>
                    <Text size="sm" fw={500}>
                      {hook.name || hook.message_name}
                    </Text>
                    {hook.correlation_expression && (
                      <Text size="xs" c="dimmed">
                        matched on <Code fz={10}>{hook.correlation_expression}</Code>
                      </Text>
                    )}
                    <LegacySigningNotice hook={hook} />
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" size="sm" styles={{ label: { textTransform: 'none' } }}>
                      {hook.message_name}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Group gap={4} wrap="nowrap">
                      <Code fz={10}>/api/v1/hooks/{hook.token.slice(0, 8)}…</Code>
                      <CopyUrl token={hook.token} />
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Switch
                      size="xs"
                      aria-label={`Switch ${hook.name || hook.message_name} ${hook.enabled ? 'off' : 'on'}`}
                      checked={hook.enabled}
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
                  <Table.Td>
                    <Tooltip label="Remove — the partner's URL stops working">
                      <ActionIcon
                        aria-label={`Delete the ${hook.name || hook.message_name} webhook`}
                        variant="subtle"
                        color="red"
                        size="sm"
                        onClick={() => removeWebhook(hook)}
                      >
                        <Trash2 size={14} />
                      </ActionIcon>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Stack>

      <Modal
        opened={formOpen}
        onClose={form.close}
        title="Add a webhook"
        size="lg"
      >
        <Stack gap="md">
          <TextInput
            label="Name"
            description="For you, not for the sender"
            placeholder="Stripe payments"
            value={name}
            onChange={(event) => setName(event.currentTarget.value)}
          />
          <TextInput
            label="Becomes the message"
            description="The BPMN message name a delivery turns into"
            placeholder="payment.received"
            value={messageName}
            onChange={(event) => setMessageName(event.currentTarget.value)}
            required
          />
          <TextInput
            label="Which process is it about?"
            description="A field in the delivered payload that identifies the waiting process — order.id, data.object.customer. Leave empty to start a new process on every delivery."
            placeholder="order.id"
            value={correlation}
            onChange={(event) => setCorrelation(event.currentTarget.value)}
          />
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={form.close}>
              Cancel
            </Button>
            <Button onClick={submit} loading={create.isPending} disabled={!messageName.trim()}>
              Create
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={justCreated !== null}
        onClose={() => setJustCreated(null)}
        title="Give these to the sender"
        size="lg"
      >
        {justCreated && (
          <Stack gap="md">
            <Alert variant="light" color="yellow" icon={<AlertTriangle size={16} />}>
              The signing secret is shown here and nowhere else, ever. Copy it now — it is stored encrypted and there
              is no way to read it back.
            </Alert>

            <Secret label="URL to post to" value={`${window.location.origin}/api/v1/hooks/${justCreated.token}`} />
            <Secret label="Signing secret" value={justCreated.secret ?? ''} />

            <WebhookSigningHelp />
          </Stack>
        )}
      </Modal>
    </Card>
  );
}

/** A value with a copy button, because these are always copied and never typed. */
function Secret({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <Text size="sm" fw={500} mb={4}>
        {label}
      </Text>
      <Group gap={4} wrap="nowrap">
        <Code block style={{ flex: 1, wordBreak: 'break-all' }}>
          {value}
        </Code>
        <CopyButton value={value}>
          {({ copied, copy }) => (
            <Tooltip label={copied ? 'Copied' : 'Copy'}>
              <ActionIcon aria-label={`Copy the ${label}`} variant="light" onClick={copy}>
                {copied ? <Check size={16} /> : <Copy size={16} />}
              </ActionIcon>
            </Tooltip>
          )}
        </CopyButton>
      </Group>
    </div>
  );
}

function CopyUrl({ token }: { token: string }) {
  const url = `${window.location.origin}/api/v1/hooks/${token}`;
  return (
    <CopyButton value={url}>
      {({ copied, copy }) => (
        <Tooltip label={copied ? 'Copied' : 'Copy the full URL'}>
          <ActionIcon aria-label="Copy the delivery URL" size="xs" variant="subtle" onClick={copy}>
            {copied ? <Check size={12} /> : <Copy size={12} />}
          </ActionIcon>
        </Tooltip>
      )}
    </CopyButton>
  );
}
