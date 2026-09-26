/**
 * Stops a webhook accepting legacy signatures now, once its sender has moved.
 *
 * Every day left in the window is a day a captured legacy delivery is still
 * acted on, and once the sender signs with v2 there is nothing to wait for. Two
 * steps, because it cannot be undone from here: a sender still signing the old
 * way is refused from that moment.
 */
import { Button, Group, Stack, Text } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useState } from 'react';

import { useCloseLegacySignatures } from '../../hooks/useWebhooks';

export function CloseLegacyWindow({ hookId, deadline }: { hookId: string; deadline: string }) {
  const [confirming, setConfirming] = useState(false);
  const close = useCloseLegacySignatures();

  const stopNow = () =>
    close.mutate(hookId, {
      onSuccess: () => notifications.show({ message: 'Legacy signatures are no longer accepted.', color: 'green' }),
      onError: (err) =>
        notifications.show({ title: 'Could not stop legacy signatures', message: err.message, color: 'red' }),
    });

  return (
    <Stack gap="xs">
      <Text size="sm" fw={600}>
        Has the sender moved to v2?
      </Text>
      <Text size="sm">
        Then stop accepting legacy signatures now rather than on {deadline}. Until then, a captured delivery signed
        the old way is still acted on.
      </Text>
      {confirming ? (
        <Group gap="xs">
          <Text size="sm">Deliveries signed the legacy way will be refused from now on.</Text>
          <Button size="xs" color="red" loading={close.isPending} onClick={stopNow}>
            Stop now
          </Button>
          <Button size="xs" variant="default" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
        </Group>
      ) : (
        <Group>
          <Button size="xs" variant="light" color="red" onClick={() => setConfirming(true)}>
            Stop accepting legacy signatures now
          </Button>
        </Group>
      )}
    </Stack>
  );
}
