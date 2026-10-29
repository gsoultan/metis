import { Loader, Modal, Stack, Text, Timeline } from '@mantine/core';

import { useServerSettingHistory } from '../../hooks/useServerSettings';
import { useTranslation } from '../../i18n/context';
import type { ServerSetting } from '../../services/domains/serverSettingsService';

/** Every change to one setting, newest first, with who made it and why. */
export function SettingHistory({ setting, onClose }: { setting: ServerSetting | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { data, isLoading } = useServerSettingHistory(setting?.name ?? null);

  return (
    <Modal opened={setting !== null} onClose={onClose} title={t('settings.server.historyOf', { label: setting?.label ?? '' })}>
      {isLoading ? (
        <Loader size="sm" />
      ) : (
        <Timeline bulletSize={14} lineWidth={2}>
          {(data ?? []).map((c) => (
            <Timeline.Item key={c.set_at + c.set_by} title={c.cleared ? t('settings.server.cleared') : setting?.secret ? t('settings.server.secretChanged') : c.value}>
              <Stack gap={0}>
                <Text size="xs">{c.reason}</Text>
                <Text size="xs" c="dimmed">
                  {t('settings.server.changedBy', { name: c.set_by, when: new Date(c.set_at).toLocaleString() })}
                </Text>
              </Stack>
            </Timeline.Item>
          ))}
        </Timeline>
      )}
    </Modal>
  );
}
