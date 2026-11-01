import { Group, Text } from '@mantine/core';
import { ShieldAlert, Server } from 'lucide-react';

import { useRuntime } from '../../hooks/useReleases';
import { useTranslation } from '../../i18n/context';

/**
 * Says which environment the page is served from, on every page, so nobody
 * changes production believing it is staging. A protected environment is
 * red; another environment is named quietly; the installation's own runtime,
 * unprotected, says nothing, as it always has.
 */
export function RuntimeBanner() {
  const { t } = useTranslation();
  const runtime = useRuntime();
  const current = runtime.data?.current;
  if (!current || (!current.protected && !current.name)) return null;

  const name = current.name || t('releases.ownRuntime');
  const Icon = current.protected ? ShieldAlert : Server;
  return (
    <Group
      role="status"
      gap="xs"
      px="xl"
      py={6}
      style={{
        backgroundColor: current.protected
          ? 'light-dark(var(--mantine-color-red-1), var(--mantine-color-red-9))'
          : 'light-dark(var(--mantine-color-blue-0), var(--mantine-color-dark-6))',
      }}
    >
      <Icon size={15} aria-hidden />
      <Text size="sm" fw={current.protected ? 600 : 400}>
        {current.protected ? t('runtime.protected', { name }) : t('runtime.environment', { name })}
      </Text>
    </Group>
  );
}
