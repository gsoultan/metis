import { Accordion, Alert, Card, Divider, Skeleton, Stack, Text, Title } from '@mantine/core';
import { RotateCcw, Server, ShieldAlert } from 'lucide-react';
import { useState } from 'react';

import { groupSettings, waitsForRestart } from '../../domain/serverSettings';
import { useServerSettings } from '../../hooks/useServerSettings';
import { useTranslation } from '../../i18n/context';
import type { ServerSetting } from '../../services/domains/serverSettingsService';
import { errorMessage } from '../../services/shared/errors';
import { EnvironmentOnlySettings } from './EnvironmentOnlySettings';
import { ServerSettingRow } from './ServerSettingRow';
import { SettingHistory } from './SettingHistory';

/**
 * The server's own settings, for a platform administrator.
 *
 * Each was only an environment variable. Most can now be changed here; the
 * environment still wins where it sets one, and what only the environment can
 * set is listed below them, closed, with the reason.
 */
export function ServerSettings() {
  const { t } = useTranslation();
  const { data, isLoading, error } = useServerSettings(true);
  const [history, setHistory] = useState<ServerSetting | null>(null);

  if (isLoading) {
    return <Skeleton h={160} radius="lg" />;
  }
  if (error) {
    return (
      <Alert icon={<ShieldAlert size={16} />} color="gray" title={t('settings.server.title')}>
        {errorMessage(error, t('settings.server.unavailable'))}
      </Alert>
    );
  }
  const settings = data?.settings ?? [];
  return (
    <Card withBorder radius="lg" p="xl" shadow="sm">
      <Stack gap="md">
        <div>
          <Title order={2} size="h5"><Server size={16} /> {t('settings.server.title')}</Title>
          <Text size="sm" c="dimmed">{t('settings.server.about')}</Text>
        </div>
        {waitsForRestart(settings) && (
          <Alert icon={<RotateCcw size={16} />} color="orange" variant="light">{t('settings.server.restartBanner')}</Alert>
        )}
        <Accordion multiple variant="separated">
          {groupSettings(settings).map(({ group, settings: inGroup }) => (
            <Accordion.Item key={group} value={group}>
              <Accordion.Control>{t(`settings.server.group.${group}`)}</Accordion.Control>
              <Accordion.Panel>
                {inGroup.map((s, i) => (
                  <div key={s.name}>
                    {i > 0 && <Divider />}
                    <ServerSettingRow setting={s} onShowHistory={setHistory} />
                  </div>
                ))}
              </Accordion.Panel>
            </Accordion.Item>
          ))}
          <Accordion.Item value="environment-only">
            <Accordion.Control>{t('settings.server.env.title')}</Accordion.Control>
            <Accordion.Panel>
              <Text size="xs" c="dimmed" mb="sm">{t('settings.server.env.about')}</Text>
              <EnvironmentOnlySettings settings={data?.environmentOnly ?? []} />
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>
      </Stack>
      <SettingHistory setting={history} onClose={() => setHistory(null)} />
    </Card>
  );
}
