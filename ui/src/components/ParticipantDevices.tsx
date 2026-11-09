import { ActionIcon, Alert, Badge, Center, Loader, Modal, Stack, Table, Text, Tooltip } from '@mantine/core';
import { AlertCircle, Trash2 } from 'lucide-react';

import { deviceDetail, deviceName } from '../domain/deviceLabels';
import { useParticipantDevices, useRemoveParticipantDevice } from '../hooks/useParticipantDevices';
import { errorMessage } from '../services/shared/errors';

interface ParticipantDevicesProps {
  person: { id: string; name: string } | null;
  onClose: () => void;
}

/**
 * The phones and browsers one person registered for push, so an administrator
 * can remove one that was lost. Devices are added by the person's own app,
 * never here.
 */
export function ParticipantDevices({ person, onClose }: ParticipantDevicesProps) {
  const devices = useParticipantDevices(person?.id ?? null);
  const remove = useRemoveParticipantDevice();
  const list = devices.data ?? [];

  return (
    <Modal opened={person !== null} onClose={onClose} title={person ? `${person.name}'s devices` : ''} size="lg">
      <Stack gap="sm">
        <Text size="sm" c="dimmed">
          Added by them from your app. Remove a lost or old phone so pushes stop going to it. Phones Firebase reports
          as uninstalled are removed on their own.
        </Text>
        {devices.isLoading && (
          <Center py="md">
            <Loader size="sm" />
          </Center>
        )}
        {devices.error && (
          <Alert color="red" icon={<AlertCircle size={18} />}>
            {errorMessage(devices.error, 'Their devices could not be read.')}
          </Alert>
        )}
        {devices.isSuccess && list.length === 0 && (
          <Text size="sm">None yet. Reminders reach them by email until their app registers a phone.</Text>
        )}
        {list.length > 0 && (
          <Table verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Device</Table.Th>
                <Table.Th>Added</Table.Th>
                <Table.Th>Last push</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {list.map((device) => (
                <Table.Tr key={device.id}>
                  <Table.Td>
                    <Text size="sm" fw={600}>{deviceName(device)}</Text>
                    <Text size="xs" c="dimmed">{deviceDetail(device)}</Text>
                  </Table.Td>
                  <Table.Td><Text size="sm">{new Date(device.created_at).toLocaleDateString()}</Text></Table.Td>
                  <Table.Td>
                    {device.last_sent_at
                      ? <Text size="sm">{new Date(device.last_sent_at).toLocaleString()}</Text>
                      : <Badge size="sm" variant="light" color="gray">None yet</Badge>}
                  </Table.Td>
                  <Table.Td ta="right">
                    <Tooltip label="Remove: pushes stop going to it">
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        aria-label={`Remove ${deviceName(device)}`}
                        loading={remove.isPending && remove.variables?.deviceId === device.id}
                        onClick={() => person && remove.mutate({ participantId: person.id, deviceId: device.id })}
                      >
                        <Trash2 size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Stack>
    </Modal>
  );
}
