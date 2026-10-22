/**
 * Registering an outgoing webhook, and handing over its secret.
 *
 * The form asks what to call it, where to send it and when. The answer carries
 * the signing secret, which is shown here once and never again: the receiver
 * needs it to check that a delivery really came from this installation.
 */
import {
  ActionIcon,
  Alert,
  Button,
  Checkbox,
  Code,
  CopyButton,
  Group,
  Modal,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { AlertTriangle, Check, Copy } from 'lucide-react';
import { useState } from 'react';

import { EVENT_GROUPS } from '../../domain/outgoingWebhooks';
import { V2_SIGNING } from '../../domain/webhookSigning';
import { useCreateOutgoingWebhook } from '../../hooks/useOutgoingWebhooks';
import type { ApiOutgoingWebhook, OutgoingWebhookEvent } from '../../services/domains/outgoingWebhookService';
import { errorMessage } from '../../services/shared/errors';

interface Props {
  opened: boolean;
  onClose: () => void;
  /** Called with the new webhook once its secret has been shown, to send it a test. */
  onSendTest: (hook: ApiOutgoingWebhook) => void;
}

export function CreateOutgoingWebhook({ opened, onClose, onSendTest }: Props) {
  const create = useCreateOutgoingWebhook();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [events, setEvents] = useState<OutgoingWebhookEvent[]>([]);

  // Held only until the dialog is dismissed. There is nowhere to fetch it from
  // afterwards, which is the point.
  const [created, setCreated] = useState<ApiOutgoingWebhook | null>(null);

  const reset = () => {
    setName('');
    setUrl('');
    setEvents([]);
  };

  const submit = async () => {
    try {
      const hook = await create.mutateAsync({ name: name.trim(), url: url.trim(), events });
      reset();
      onClose();
      setCreated(hook);
    } catch (err: unknown) {
      notifications.show({
        title: 'Could not create the webhook',
        message: errorMessage(err, 'Something went wrong.'),
        color: 'red',
      });
    }
  };

  const ready = name.trim() !== '' && url.trim() !== '' && events.length > 0;

  return (
    <>
      <Modal opened={opened} onClose={onClose} title="New outgoing webhook" size="lg">
        <Stack gap="md">
          <TextInput
            label="Name"
            description="For you. The receiver never sees it."
            placeholder="Finance ledger"
            value={name}
            onChange={(event) => setName(event.currentTarget.value)}
            required
          />
          <TextInput
            label="Where to send it"
            description="Must start with https://. Addresses inside your network, such as localhost or 10.x.x.x, are refused unless your administrator allows them."
            placeholder="https://ledger.example.com/hooks/metis"
            value={url}
            onChange={(event) => setUrl(event.currentTarget.value)}
            required
          />
          <Checkbox.Group
            label="When to send it"
            value={events}
            onChange={(value) => setEvents(value as OutgoingWebhookEvent[])}
            required
          >
            <Stack gap="sm" mt="xs">
              {EVENT_GROUPS.map((group) => (
                <Stack key={group.title} gap={6}>
                  <Text size="xs" fw={600} c="dimmed" tt="uppercase">
                    {group.title}
                  </Text>
                  {group.choices.map((choice) => (
                    <Checkbox key={choice.event} value={choice.event} label={choice.label} />
                  ))}
                </Stack>
              ))}
            </Stack>
          </Checkbox.Group>
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={submit} loading={create.isPending} disabled={!ready}>
              Create webhook
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={created !== null}
        onClose={() => setCreated(null)}
        title={created ? `${created.name} is ready` : ''}
        size="lg"
      >
        {created && (
          <Stack gap="md">
            <Alert variant="light" color="yellow" icon={<AlertTriangle size={16} />} title="Copy the signing secret now">
              This is the only time it is shown. Put it in the receiver&apos;s settings so it can check that each
              delivery really came from Metis.
            </Alert>
            <SigningSecret value={created.secret ?? ''} />
            <Text size="xs" c="dimmed">
              Each delivery carries <Code fz={10}>{V2_SIGNING.signatureHeader}</Code>,{' '}
              <Code fz={10}>{V2_SIGNING.timestampHeader}</Code> and <Code fz={10}>{V2_SIGNING.deliveryIdHeader}</Code>.
              The signature is <Code fz={10}>{V2_SIGNING.prefix}</Code> and the hex HMAC-SHA256, with the secret, of{' '}
              <Code fz={10}>{V2_SIGNING.signedString}</Code>. A retry keeps the same delivery ID, so the receiver can
              ignore one it has already handled.
            </Text>
            <Group justify="flex-end">
              <Button
                variant="default"
                onClick={() => {
                  const hook = created;
                  setCreated(null);
                  onSendTest(hook);
                }}
              >
                Send a test
              </Button>
              <Button onClick={() => setCreated(null)}>I&apos;ve saved it</Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </>
  );
}

function SigningSecret({ value }: { value: string }) {
  return (
    <div>
      <Text size="sm" fw={500} mb={4}>
        Signing secret
      </Text>
      <Group gap={4} wrap="nowrap">
        <Code block style={{ flex: 1, wordBreak: 'break-all' }}>
          {value}
        </Code>
        <CopyButton value={value}>
          {({ copied, copy }) => (
            <Tooltip label={copied ? 'Copied' : 'Copy'}>
              <ActionIcon aria-label="Copy the signing secret" variant="light" onClick={copy}>
                {copied ? <Check size={16} /> : <Copy size={16} />}
              </ActionIcon>
            </Tooltip>
          )}
        </CopyButton>
      </Group>
    </div>
  );
}
