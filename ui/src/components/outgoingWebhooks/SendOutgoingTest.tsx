/**
 * Trying a receiver: one real, signed request, marked as a test.
 *
 * What came back is shown three ways, because three people read it: the
 * request exactly as it went out, for whoever runs the receiver; the answer,
 * for whoever set the webhook up; and how the signature was made, for the
 * developer whose check refused it.
 */
import { Alert, Badge, Button, Code, Group, Modal, Select, Stack, Tabs, Text } from '@mantine/core';
import { Send } from 'lucide-react';
import { useState } from 'react';

import { EVENT_GROUPS, eventLabel, prettyBody, troubleHint } from '../../domain/outgoingWebhooks';
import { V2_SIGNING } from '../../domain/webhookSigning';
import { useSendOutgoingWebhookTest } from '../../hooks/useOutgoingWebhooks';
import type { ApiOutgoingTest, ApiOutgoingWebhook, OutgoingWebhookEvent } from '../../services/domains/outgoingWebhookService';
import { errorMessage } from '../../services/shared/errors';

interface Props {
  hook: ApiOutgoingWebhook | null;
  onClose: () => void;
}

export function SendOutgoingTest({ hook, onClose }: Props) {
  return (
    <Modal opened={hook !== null} onClose={onClose} title={hook ? `Send a test to ${hook.name}` : ''} size="xl">
      {/* Keyed so another webhook starts with a clean result. */}
      {hook && <TestPanel key={hook.id} hook={hook} />}
    </Modal>
  );
}

function TestPanel({ hook }: { hook: ApiOutgoingWebhook }) {
  const send = useSendOutgoingWebhookTest(hook.id);
  const [event, setEvent] = useState<OutgoingWebhookEvent | null>(hook.events[0] ?? null);

  // The webhook's own events first: those are what its receiver is waiting for.
  const options = EVENT_GROUPS.flatMap((group) => group.choices)
    .sort((a, b) => Number(!hook.events.includes(a.event)) - Number(!hook.events.includes(b.event)))
    .map((choice) => ({
      value: choice.event,
      label: hook.events.includes(choice.event) ? `${choice.short} (sample data)` : `${choice.short} (sample data, not sent to it yet)`,
    }));

  return (
    <Stack gap="md">
      <Text size="sm" c="dimmed">
        Sends one real, signed request to the receiver, marked as a test. Nothing in your processes changes. It is
        sent even while the webhook is switched off.
      </Text>
      <Group align="flex-end">
        <Select
          label="Sample event"
          data={options}
          value={event}
          onChange={(value) => setEvent(value as OutgoingWebhookEvent | null)}
          allowDeselect={false}
          style={{ flex: 1 }}
        />
        <Button leftSection={<Send size={14} />} loading={send.isPending} onClick={() => send.mutate(event ?? undefined)}>
          Send test
        </Button>
      </Group>

      {send.isError && (
        <Alert color="red" variant="light" title="The test was not sent">
          {errorMessage(send.error)}
        </Alert>
      )}
      {send.data && <TestResult test={send.data} />}
    </Stack>
  );
}

function TestResult({ test }: { test: ApiOutgoingTest }) {
  const { response } = test;
  const hint = troubleHint(response.status_code, response.error);
  const headline = response.status_code ? String(response.status_code) : 'No answer';

  return (
    <Stack gap="sm">
      <Group gap="sm">
        <Badge color={test.accepted ? 'green' : 'red'} variant="filled" size="lg">
          {headline}
        </Badge>
        <div>
          <Text size="sm" fw={600}>
            {test.accepted ? 'The receiver accepted it' : 'The receiver did not accept it'}
          </Text>
          <Text size="xs" c="dimmed">
            {eventLabel(test.event)} · {test.duration_ms} ms
          </Text>
        </div>
      </Group>
      {!test.accepted && (response.error || hint) && (
        <Alert color="yellow" variant="light">
          {response.error && <Text size="sm">{capitalised(response.error)}.</Text>}
          {hint && <Text size="sm">{hint}</Text>}
        </Alert>
      )}

      <Tabs defaultValue="request" variant="outline">
        <Tabs.List>
          <Tabs.Tab value="request">Request</Tabs.Tab>
          <Tabs.Tab value="response">Response</Tabs.Tab>
          <Tabs.Tab value="signature">Signature</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="request" pt="sm">
          <Code block fz={11}>
            {requestText(test)}
          </Code>
        </Tabs.Panel>
        <Tabs.Panel value="response" pt="sm">
          {response.status_code ? (
            <Code block fz={11}>
              {response.body || '(an empty body)'}
            </Code>
          ) : (
            <Text size="sm" c="dimmed">
              Nothing came back{response.error ? `: ${response.error}` : ''}.
            </Text>
          )}
        </Tabs.Panel>
        <Tabs.Panel value="signature" pt="sm">
          <SignatureSteps test={test} />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}

/** The request as it went out: the line, every header, and the body. */
function requestText(test: ApiOutgoingTest): string {
  const { request } = test;
  const headers = Object.entries(request.headers)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, value]) => `${name}: ${value}`);
  return [`${request.method} ${request.url}`, ...headers, '', prettyBody(request.body)].join('\n');
}

function SignatureSteps({ test }: { test: ApiOutgoingTest }) {
  const timestamp = headerOf(test, V2_SIGNING.timestampHeader);
  const deliveryId = headerOf(test, V2_SIGNING.deliveryIdHeader);
  return (
    <Stack gap="xs">
      <Text size="sm">Join the timestamp, the delivery ID and the exact body with dots:</Text>
      <Code block fz={11}>
        {`${timestamp}.${deliveryId}.<the body, byte for byte>`}
      </Code>
      <Text size="sm">
        HMAC-SHA256 that text with the signing secret, and send it as hex after{' '}
        <Code fz={11}>{V2_SIGNING.prefix}</Code> in <Code fz={11}>{V2_SIGNING.signatureHeader}</Code>. The receiver
        recomputes it over the raw body before parsing it, and rejects a timestamp more than{' '}
        {V2_SIGNING.toleranceMinutes} minutes from its clock.
      </Text>
    </Stack>
  );
}

/** A header's value whatever case it was sent in. */
function headerOf(test: ApiOutgoingTest, name: string): string {
  const wanted = name.toLowerCase();
  const found = Object.entries(test.request.headers).find(([key]) => key.toLowerCase() === wanted);
  return found?.[1] ?? '';
}

function capitalised(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}
