import { ActionIcon, Alert, Group, Paper, Stack, Table, Text, Title, Tooltip } from '@mantine/core';
import { AlertCircle, Smartphone, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';

import { deviceDetail, deviceName } from '../domain/deviceLabels';
import { useTranslation } from '../i18n/context';
import { PeopleSessionEnded, personDeviceService, type PersonDevice } from '../services/domains/peopleService';
import { errorMessage } from '../services/shared/errors';

/**
 * The phones and browsers a person gets push notifications on. Their app adds
 * a device when they sign in there; here they see them and remove one they no
 * longer use.
 */
export function PersonDevices({ token, onEnded }: { token: string; onEnded: () => void }) {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<PersonDevice[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const failed = useCallback(
    (failure: unknown) => {
      if (failure instanceof PeopleSessionEnded) {
        onEnded();
        return;
      }
      setError(errorMessage(failure, t('people.devices.failed')));
    },
    [onEnded, t],
  );

  useEffect(() => {
    const controller = new AbortController();
    personDeviceService
      .list(token, controller.signal)
      .then((list) => {
        if (!controller.signal.aborted) setDevices(list);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) failed(failure);
      });
    return () => controller.abort();
  }, [token, failed]);

  const remove = async (id: string) => {
    setBusy(true);
    setError(null);
    try {
      await personDeviceService.remove(token, id);
      setDevices(await personDeviceService.list(token));
    } catch (failure) {
      failed(failure);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Paper withBorder p="lg" radius="md">
      <Stack gap="sm">
        <Group gap="xs">
          <Smartphone size={18} />
          <Title order={4}>{t('people.devices.title')}</Title>
        </Group>
        <Text size="sm" c="dimmed">{t('people.devices.about')}</Text>
        {error && (
          <Alert color="red" icon={<AlertCircle size={18} />} withCloseButton onClose={() => setError(null)}>
            {error}
          </Alert>
        )}
        {devices !== null && devices.length === 0 && <Text size="sm">{t('people.devices.none')}</Text>}
        {devices !== null && devices.length > 0 && (
          <Table verticalSpacing="xs">
            <Table.Tbody>
              {devices.map((device) => (
                <Table.Tr key={device.id}>
                  <Table.Td>
                    <Text size="sm" fw={600}>{deviceName(device)}</Text>
                    <Text size="xs" c="dimmed">{deviceDetail(device)}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="xs" c="dimmed">
                      {t('people.devices.added', { date: new Date(device.created_at).toLocaleDateString() })}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="right">
                    <Tooltip label={t('people.devices.remove')}>
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        aria-label={t('people.devices.removeNamed', { name: deviceName(device) })}
                        disabled={busy}
                        onClick={() => remove(device.id)}
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
    </Paper>
  );
}
