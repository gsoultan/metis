import { Button, Group, Notification, Stack, Text } from '@mantine/core';
import { CloudOff, RefreshCw, Upload } from 'lucide-react';

import { useOutbox } from './useOutbox';
import { useServerUpgrade } from './useServerUpgrade';
import { useOnlineStatus, useServiceWorker } from './useServiceWorker';

/**
 * The things the service worker needs to say out loud.
 *
 * The server was upgraded, a new version is waiting, and the connection has
 * gone. All are fixed to the
 * bottom of the screen rather than shown as a toast, because a toast that
 * disappears after four seconds is no use for a state that persists.
 */
export function ServiceWorkerPrompt() {
  const { updateReady, update, dismiss } = useServiceWorker();
  const online = useOnlineStatus();
  const { summary, flush, unowned, unownedSummary, discardUnowned } = useOutbox();
  const server = useServerUpgrade();
  const upgraded = server.kind === 'upgraded';

  // Through the waiting worker when there is one, so the reload is served by
  // the new worker and not the old one's cache; a plain reload otherwise.
  const reloadIntoNewVersion = () => {
    if (updateReady) {
      update();
    } else {
      window.location.reload();
    }
  };

  if (!upgraded && !updateReady && online && summary === '' && unownedSummary === '') {
    return null;
  }

  return (
    <Stack
      gap="xs"
      style={{ position: 'fixed', bottom: 16, right: 16, zIndex: 400, maxWidth: 360 }}
    >
      {!online && (
        <Notification
          icon={<CloudOff size={18} />}
          color="orange"
          title="You are offline"
          withCloseButton={false}
        >
          <Text size="sm">
            You can still read what has already loaded, and complete the tasks in
            your inbox. They will be sent when you are back online.
          </Text>
        </Notification>
      )}

      {/*
        Somebody's unsent work. Shown whether online or not: if it is still here
        while the connection is back, something is wrong and saying nothing
        would leave them believing an approval had gone through.
      */}
      {summary !== '' && (
        <Notification
          icon={<Upload size={18} />}
          color={online ? 'blue' : 'gray'}
          title={summary}
          withCloseButton={false}
        >
          <Stack gap="xs">
            <Text size="sm">
              {online
                ? 'Sending them now. They are kept here until the server confirms each one.'
                : 'Kept on this device until you are back online.'}
            </Text>
            {online && (
              <Group gap="xs">
                <Button size="xs" variant="light" onClick={() => void flush()}>
                  Try again now
                </Button>
              </Group>
            )}
          </Stack>
        </Notification>
      )}

      {/*
        Work queued before this device recorded who did it. It is never sent,
        because it could be anybody's, and until now it was never shown
        either: somebody's approval sat here with nothing saying it had not
        gone through. Named, so whoever made it knows to do it again.
      */}
      {unownedSummary !== '' && (
        <Notification
          icon={<Upload size={18} />}
          color="orange"
          title={unownedSummary}
          withCloseButton={false}
        >
          <Stack gap="xs">
            <Text size="sm">
              {unowned.length === 1 ? 'It was' : 'They were'} saved on this device before it
              recorded who made each change, so {unowned.length === 1 ? 'it' : 'they'} cannot
              be sent in anybody's name. Do {unowned.length === 1 ? 'it' : 'them'} again if
              still needed:
            </Text>
            <Text size="sm" component="ul" style={{ margin: 0, paddingLeft: 18 }}>
              {unowned.map((entry) => (
                <li key={entry.key}>{entry.label}</li>
              ))}
            </Text>
            <Group gap="xs">
              <Button size="xs" variant="light" color="orange" onClick={() => void discardUnowned()}>
                Discard
              </Button>
            </Group>
          </Stack>
        </Notification>
      )}

      {/*
        The server under this page was upgraded. Not dismissable: this page is
        the old interface talking to a new server, and the reload at the next
        change of page happens whether this is read or not.
      */}
      {upgraded && (
        <Notification
          icon={<RefreshCw size={18} />}
          color="blue"
          title={`Metis is now ${server.label}`}
          withCloseButton={false}
        >
          <Stack gap="xs">
            <Text size="sm">
              This page is still the previous version. It reloads into the new one when you
              move to another page, or reload now if you have nothing unsaved here.
            </Text>
            <Group gap="xs">
              <Button size="xs" onClick={reloadIntoNewVersion}>
                Reload now
              </Button>
            </Group>
          </Stack>
        </Notification>
      )}

      {updateReady && !upgraded && (
        <Notification
          icon={<RefreshCw size={18} />}
          color="blue"
          title="A new version is ready"
          onClose={dismiss}
        >
          <Stack gap="xs">
            <Text size="sm">
              Reload when you are at a good stopping point. Anything you are part
              way through typing is not saved yet.
            </Text>
            <Group gap="xs">
              <Button size="xs" onClick={update}>
                Reload now
              </Button>
              <Button size="xs" variant="subtle" color="gray" onClick={dismiss}>
                Later
              </Button>
            </Group>
          </Stack>
        </Notification>
      )}
    </Stack>
  );
}
