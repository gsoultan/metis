/**
 * What a sender has to send, in the words the developer at the other end needs.
 *
 * Shown when a webhook is created, beside the secret it signs with, and to
 * whoever is moving an older webhook's sender off legacy signatures. The names
 * come from V2_SIGNING so the help cannot drift from what the server checks.
 */
import { Code, List, Stack, Text } from '@mantine/core';

import { V2_SIGNING } from '../../domain/webhookSigning';

export function WebhookSigningHelp() {
  return (
    <Stack gap={6}>
      <Text size="xs" c="dimmed">
        Every delivery carries three headers, signed with the webhook&apos;s secret:
      </Text>
      <List size="xs" spacing={4}>
        <List.Item>
          <Code fz={10}>{V2_SIGNING.timestampHeader}</Code> — when it is sent, in Unix seconds
        </List.Item>
        <List.Item>
          <Code fz={10}>{V2_SIGNING.deliveryIdHeader}</Code> — the sender&apos;s own ID for the event, the same on every
          retry
        </List.Item>
        <List.Item>
          <Code fz={10}>{V2_SIGNING.signatureHeader}</Code> — <Code fz={10}>{V2_SIGNING.prefix}</Code> followed by the
          hex HMAC-SHA256, with the secret, of:
        </List.Item>
      </List>
      <Code block fz={11}>
        {V2_SIGNING.signedString}
      </Code>
      <Text size="xs" c="dimmed">
        Sign each attempt as it is sent, retries included. A delivery more than {V2_SIGNING.toleranceMinutes} minutes
        from this server&apos;s clock is refused, and one whose ID has been seen before is not acted on twice.
      </Text>
    </Stack>
  );
}
