import { Alert, Anchor, Badge, Button, Group, List, Paper, SimpleGrid, Stack, Text, Title } from '@mantine/core';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowUpCircle, CheckCircle2, CircleAlert, RefreshCw } from 'lucide-react';

import { PageHeader } from '../components/PageHeader';
import { readBuildIdentity } from '../domain/buildVersion';
import { checkedLabel, installGuidance, migrationLines, migrationsLabel } from '../domain/upgrades';
import { useTranslation } from '../i18n/context';
import { upgradeService, type UpgradeStatus } from '../services/domains/upgradeService';
import { UPGRADE_STATUS_KEY } from './upgradeStatusKey';

const UPGRADING_NOTES = 'https://github.com/gsoultan/metis/blob/main/docs/upgrading.md';

/**
 * Newer releases of Metis, for a platform administrator.
 *
 * Read-only: it says what is newer, what it would change in the database and
 * how this installation is upgraded. Installing is done on the host, by the
 * upgrade helper or by hand.
 */
export function Upgrades() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: status, error } = useQuery({
    queryKey: UPGRADE_STATUS_KEY,
    queryFn: ({ signal }) => upgradeService.getStatus(signal),
  });
  const check = useMutation({
    mutationFn: () => upgradeService.checkNow(),
    onSuccess: (next) => queryClient.setQueryData(UPGRADE_STATUS_KEY, next),
  });

  return (
    <Stack gap="xl">
      <PageHeader title={t('page.upgrades.title')} description={t('page.upgrades.subtitle')} />

      {error !== null && (
        <Alert color="red" icon={<CircleAlert size={18} />} title="The upgrade status could not be read">
          {error.message}
        </Alert>
      )}

      {status !== undefined && (
        <>
          <Versions status={status} checking={check.isPending} onCheck={() => check.mutate()} />
          {check.error !== null && (
            <Alert color="red" icon={<CircleAlert size={18} />}>
              {check.error.message}
            </Alert>
          )}
          <NewestRelease status={status} />
        </>
      )}
    </Stack>
  );
}

function Versions({ status, checking, onCheck }: { status: UpgradeStatus; checking: boolean; onCheck: () => void }) {
  const running = readBuildIdentity(status.current);
  return (
    <Paper p="xl" radius="lg" withBorder shadow="sm">
      <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="lg">
        <Stack gap={2}>
          <Text size="xs" c="dimmed">Running</Text>
          <Text fw={700} size="xl">{running.label}</Text>
          <Text size="xs" c="dimmed">Database schema {status.schema_version}</Text>
        </Stack>
        <Stack gap={2}>
          <Text size="xs" c="dimmed">Newest release</Text>
          <Text fw={700} size="xl">{status.newest?.tag ?? (status.enabled ? running.label : '—')}</Text>
          <Text size="xs" c="dimmed">
            {status.newest !== undefined
              ? `Published ${new Date(status.newest.published_at).toLocaleDateString()}`
              : status.enabled
                ? 'You are up to date'
                : 'Checking is turned off'}
          </Text>
        </Stack>
        <Stack gap={2}>
          <Text size="xs" c="dimmed">Last checked</Text>
          <Text fw={600}>{status.enabled ? checkedLabel(status.checked_at, new Date()) : 'Never'}</Text>
          {status.enabled && (
            <Group gap={4}>
              <Button size="compact-sm" variant="subtle" leftSection={<RefreshCw size={14} />} loading={checking} onClick={onCheck}>
                Check now
              </Button>
            </Group>
          )}
        </Stack>
      </SimpleGrid>
      {!status.enabled && (
        <Text size="sm" c="dimmed" mt="md">
          This installation does not ask GitHub for new releases, because METIS_UPGRADE_CHECK is false.
        </Text>
      )}
      {!status.current_is_release && (
        <Text size="sm" c="dimmed" mt="md">
          This build is not from a release tag, so it is never offered an upgrade.
        </Text>
      )}
      {status.error !== undefined && status.error !== '' && (
        <Alert color="orange" mt="md" icon={<CircleAlert size={18} />} title="The last check did not reach GitHub">
          {sentence(status.error)} What is shown is from the last check that did.
        </Alert>
      )}
    </Paper>
  );
}

function NewestRelease({ status }: { status: UpgradeStatus }) {
  const newest = status.newest;
  if (newest === undefined) {
    return null;
  }
  const migrations = migrationsLabel(status);
  const lines = migrationLines(status.pending);
  return (
    <Paper p="xl" radius="lg" withBorder shadow="sm">
      <Group justify="space-between" align="flex-start" mb="md">
        <Group gap="xs">
          <ArrowUpCircle size={20} />
          <Title order={2} size="h4">{newest.tag}</Title>
          {newest.prerelease && <Badge color="grape" variant="light">Release candidate</Badge>}
          {migrations !== null && (
            <Badge color={lines.length > 0 ? 'yellow' : 'green'} variant="light">{migrations}</Badge>
          )}
        </Group>
        <Group gap="xs">
          <Button component="a" href={newest.url} target="_blank" rel="noreferrer noopener" variant="default">
            Release notes
          </Button>
          <Button component="a" href={UPGRADING_NOTES} target="_blank" rel="noreferrer noopener" variant="default">
            Upgrading notes
          </Button>
        </Group>
      </Group>

      {!status.reachable && newest.manifest !== undefined && (
        <Alert color="red" icon={<CircleAlert size={18} />} mb="md" title="This database cannot go straight to this release">
          The database is at schema {status.schema_version} and {newest.tag} upgrades from {newest.manifest.oldest_schema} or
          later. Start Metis {newest.manifest.via_release} against it once first.
        </Alert>
      )}

      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="xl">
        <Stack gap="xs">
          <Text size="xs" c="dimmed">What changes in the database</Text>
          {newest.manifest === undefined ? (
            <Text size="sm">{sentence(newest.manifest_error ?? 'this release does not say.')}</Text>
          ) : lines.length === 0 ? (
            <Group gap={6}>
              <CheckCircle2 size={16} color="var(--mantine-color-green-6)" />
              <Text size="sm">Nothing. The database stays as it is.</Text>
            </Group>
          ) : (
            <>
              <List size="sm" spacing={4}>
                {lines.map((line) => (
                  <List.Item key={line}>{line}</List.Item>
                ))}
              </List>
              <Text size="xs" c="dimmed">
                Migrations run once, at the first start, and only forward. Going back afterwards means restoring the backup
                taken before the upgrade, so take one and rehearse with scripts/upgrade-rehearsal.sh.
              </Text>
            </>
          )}
        </Stack>
        <Stack gap="xs">
          <Text size="xs" c="dimmed">How to upgrade this installation</Text>
          <Text size="sm">{installGuidance(status.install)}</Text>
          {newest.notes !== '' && (
            <Text size="sm" c="dimmed" lineClamp={6} style={{ whiteSpace: 'pre-line' }}>
              {newest.notes}
            </Text>
          )}
          <Anchor size="sm" href={newest.url} target="_blank" rel="noreferrer noopener">
            Read the full release notes on GitHub
          </Anchor>
        </Stack>
      </SimpleGrid>
    </Paper>
  );
}

/** The server's errors are Go errors, lower case and unpunctuated; on the page they are sentences. */
function sentence(text: string): string {
  const trimmed = text.trim();
  if (trimmed === '') {
    return trimmed;
  }
  const capital = trimmed[0].toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capital) ? capital : `${capital}.`;
}
