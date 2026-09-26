/**
 * Says, under a webhook's name, that it still accepts legacy signatures and
 * until when — and how to move its sender before then.
 *
 * Nothing at all for a webhook that accepts v2 only, which is every webhook
 * created since v2 existed and every older one whose window has closed: a line
 * that is always there stops being read.
 */
import { Anchor, Divider, Group, Modal, Stack, Text } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { AlertTriangle } from 'lucide-react';

import { legacySigningOf } from '../../domain/webhookSigning';
import type { ApiWebhook } from '../../services/domains/webhookService';
import { CloseLegacyWindow } from './CloseLegacyWindow';
import { WebhookSigningHelp } from './WebhookSigningHelp';

const DEADLINE_FORMAT: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short' };

// Colour goes on the icon, never on the words. Mantine's yellow and red at a
// size this small miss AA contrast on the app's surfaces — yellow badly — and
// the sentence already says everything the colour does, so the icon is
// decorative and the text keeps the theme's body colour. Urgency is carried
// by weight and by the words "N days left".
const ICON_COLOR = { calm: 'var(--mantine-color-yellow-7)', urgent: 'var(--mantine-color-red-7)' };

export function LegacySigningNotice({ hook }: { hook: ApiWebhook }) {
  const [helpOpen, help] = useDisclosure(false);
  const signing = legacySigningOf(hook);
  if (signing.state !== 'accepting') return null;

  const who = hook.name || hook.message_name;
  const deadline = signing.until.toLocaleString(undefined, DEADLINE_FORMAT);
  return (
    <>
      <Group gap={4} wrap="nowrap" align="flex-start" mt={2}>
        <AlertTriangle
          size={12}
          aria-hidden
          color={signing.urgent ? ICON_COLOR.urgent : ICON_COLOR.calm}
          style={{ flexShrink: 0, marginTop: 2 }}
        />
        <Text size="xs" fw={signing.urgent ? 600 : undefined}>
          Still accepts legacy signatures until {deadline} ({signing.remaining}).{' '}
          <Anchor component="button" type="button" size="xs" onClick={help.open}>
            How to move the sender to v2
          </Anchor>
        </Text>
      </Group>

      <Modal
        opened={helpOpen}
        onClose={help.close}
        title={`Moving ${who} to v2 signatures`}
        size="lg"
      >
        <Stack gap="md">
          <Text size="sm">
            A legacy signature covers the request body alone, so anyone who captures one delivery can send it again
            and have it acted on again. This webhook accepts them until {deadline}; after that, deliveries signed that
            way are refused. Send these instructions to whoever runs the sending system — the secret stays the same.
          </Text>
          <WebhookSigningHelp />
          <Divider />
          <CloseLegacyWindow hookId={hook.id} deadline={deadline} />
        </Stack>
      </Modal>
    </>
  );
}
