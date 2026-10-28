import { Badge, Table, Text } from '@mantine/core';

import { useTranslation } from '../../i18n/context';
import type { EnvironmentOnlySetting } from '../../services/domains/serverSettingsService';

/**
 * The settings only the server's environment sets: shown so an administrator
 * can see what the server runs with and why it is not theirs to change here,
 * never editable.
 */
export function EnvironmentOnlySettings({ settings }: { settings: EnvironmentOnlySetting[] }) {
  const { t } = useTranslation();
  return (
    <Table.ScrollContainer minWidth={640}>
      <Table verticalSpacing="xs" striped>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>{t('settings.server.env.name')}</Table.Th>
            <Table.Th>{t('settings.server.env.value')}</Table.Th>
            <Table.Th>{t('settings.server.env.why')}</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {settings.map((s) => (
            <Table.Tr key={s.name}>
              <Table.Td>
                <Text size="xs" ff="monospace">{s.name}</Text>
                <Badge size="xs" variant="light" color={s.class === 'guardrail' ? 'red' : 'gray'}>
                  {t(`settings.server.env.class.${s.class}`)}
                </Badge>
              </Table.Td>
              <Table.Td>
                <Text size="xs" ff="monospace">
                  {!s.is_set ? t('settings.server.notSet') : s.secret ? t('settings.server.secretSet') : s.value || '""'}
                </Text>
              </Table.Td>
              <Table.Td><Text size="xs">{s.reason}</Text></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
