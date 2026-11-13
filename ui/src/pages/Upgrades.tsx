import { Alert, Anchor, Badge, Button, Group, List, Modal, Paper, SimpleGrid, Stack, Text, TextInput, Timeline, Title } from '@mantine/core';
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowUpCircle, CheckCircle2, CircleAlert, RefreshCw } from 'lucide-react';

import { PageHeader } from '../components/PageHeader';
import { readBuildIdentity } from '../domain/buildVersion';
import {
  checkedLabel,
  installGuidance,
  migrationLines,
  migrationsLabel,
  progressView,
  upgradeInFlight,
} from '../domain/upgrades';
import { useTranslation } from '../i18n/context';
import { upgradeService, type UpgradeProgress, type UpgradeStatus } from '../services/domains/upgradeService';
import { UPGRADE_STATUS_KEY } from './upgradeStatusKey';

const UPGRADING_NOTES = 'https://github.com/gsoultan/metis/blob/main/docs/upgrading.md';

/**
 * Newer releases of Metis, for a platform administrator.
 *
 * It says what is newer, what it would change in the database and how this
 * installation is upgraded. On a package install with the upgrade trigger
 * enabled it can ask for the upgrade: the server leaves a request, and the
 * helper on the host verifies, backs up and installs. While one is in flight
 * the page asks every few seconds, and through the restart; the tab reloads
 * into the new version when it answers (see useServerUpgrade).
 */
export function Upgrades() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: status, error } = useQuery({
    queryKey: UPGRADE_STATUS_KEY,
    queryFn: ({ signal }) => upgradeService.getStatus(signal),
    refetchInterval: (query) => (upgradeInFlight(query.state.data) ? 3_000 : false),
  });
  const inFlight = upgradeInFlight(status);
  const check = useMutation({
    mutationFn: () => upgradeService.checkNow(),
    onSuccess: (next) => queryClient.setQueryData(UPGRADE_STATUS_KEY, next),
  });

  return (
    <Stack gap="xl">
      <PageHeader title={t('page.upgrades.title')} description={t('page.upgrades.subtitle')} />

      {error !== null && !inFlight && (
        <Alert color="red" icon={<CircleAlert size={18} />} title="The upgrade status could not be read">
          {error.message}
        </Alert>
      )}
      {error !== null && inFlight && (
        <Alert color="blue" icon={<RefreshCw size={18} />} title="Metis is restarting">
          The server is not answering while the new release starts. This page keeps asking and reloads when it is back.
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
          <Progress status={status} />
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
          <StartUpgrade status={status} />
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

const OUTCOME_LABEL: Record<UpgradeProgress['outcome'], string> = {
  running: 'Running',
  done: 'Done',
  refused: 'Refused',
  rolled_back: 'Rolled back',
  needs_restore: 'Needs a restore',
};

/** The last upgrade: what it is doing, or how it ended, step by step. */
function Progress({ status }: { status: UpgradeStatus }) {
  const view = progressView(status);
  if (view === null) {
    return null;
  }
  const steps = status.progress?.steps ?? [];
  return (
    <Alert color={view.color} icon={view.color === 'blue' ? <RefreshCw size={18} /> : <CircleAlert size={18} />} title={view.title}>
      <Stack gap="sm">
        {view.body !== '' && <Text size="sm">{sentence(view.body)}</Text>}
        {status.requested === undefined && status.progress !== undefined && steps.length > 0 && (
          <Timeline active={steps.length - 1} bulletSize={14} lineWidth={2}>
            {steps.map((step) => (
              <Timeline.Item key={`${step.name}-${step.at}`} title={step.name}>
                <Text size="xs" c="dimmed">
                  {new Date(step.at).toLocaleTimeString()}
                  {step.detail !== undefined && step.detail !== '' ? ` · ${step.detail}` : ''}
                </Text>
              </Timeline.Item>
            ))}
          </Timeline>
        )}
        {status.progress !== undefined && status.requested === undefined && (
          <Text size="xs" c="dimmed">
            {OUTCOME_LABEL[status.progress.outcome]}
            {status.progress.requested_by !== undefined && status.progress.requested_by !== ''
              ? ` · asked for by ${status.progress.requested_by}`
              : ''}
          </Text>
        )}
      </Stack>
    </Alert>
  );
}

/**
 * The Upgrade button, offered only when the server says an upgrade can start;
 * otherwise the reason, which also says what to do instead. The release is
 * typed again to confirm, so a stray click upgrades nothing.
 */
function StartUpgrade({ status }: { status: UpgradeStatus }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState('');
  const newest = status.newest;
  const start = useMutation({
    mutationFn: (to: string) => upgradeService.start(to, typed.trim()),
    onSuccess: (next) => {
      queryClient.setQueryData(UPGRADE_STATUS_KEY, next);
      setOpen(false);
      setTyped('');
    },
  });
  if (newest === undefined || status.install !== 'package') {
    return null;
  }
  if (!status.can_start) {
    return status.cannot_start !== undefined && status.cannot_start !== '' && !upgradeInFlight(status) ? (
      <Text size="sm" c="dimmed">
        {sentence(status.cannot_start)}
      </Text>
    ) : null;
  }
  const close = () => {
    setOpen(false);
    setTyped('');
    start.reset();
  };
  return (
    <>
      <Group>
        <Button leftSection={<ArrowUpCircle size={16} />} onClick={() => setOpen(true)}>
          Upgrade to {newest.tag}
        </Button>
      </Group>
      <Modal opened={open} onClose={close} title={`Upgrade to ${newest.tag}`} centered>
        <Stack gap="md">
          <Text size="sm">The upgrade service on this host will:</Text>
          <List size="sm" spacing={4} type="ordered">
            <List.Item>check that {newest.tag} is signed by the Metis release workflow and that every file matches;</List.Item>
            <List.Item>back the database up with pg_dump;</List.Item>
            <List.Item>stop Metis, install the package and start it again, which takes Metis offline for a minute or two;</List.Item>
            <List.Item>put {status.current} back if {newest.tag} does not come up before it migrates the database.</List.Item>
          </List>
          {(status.pending ?? []).length > 0 && (
            <Text size="sm" c="dimmed">
              {migrationsLabel(status)} will run at the first start. After that, going back means restoring the backup.
            </Text>
          )}
          <TextInput
            label={`Type ${newest.tag} to confirm`}
            value={typed}
            onChange={(event) => setTyped(event.currentTarget.value)}
            autoComplete="off"
            data-autofocus
          />
          {start.error !== null && (
            <Alert color="red" icon={<CircleAlert size={18} />}>
              {sentence(start.error.message)}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button
              color="orange"
              disabled={typed.trim() !== newest.tag}
              loading={start.isPending}
              onClick={() => start.mutate(newest.tag)}
            >
              Upgrade now
            </Button>
          </Group>
        </Stack>
      </Modal>
    </>
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
