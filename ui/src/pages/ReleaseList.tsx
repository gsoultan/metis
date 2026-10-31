import { Badge, Button, Card, Group, Modal, Select, Stack, Table, Text, TextInput, Textarea } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { PackageCheck } from 'lucide-react';
import { useState } from 'react';

import { PageHeader, PageShell } from '../components/layout';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import {
  OWN_RUNTIME_ID,
  canRollBack,
  parseKeys,
  releaseActions,
  releaseStateColor,
  runtimeLabel,
  type Release,
} from '../domain/release';
import { useReleaseActions, useReleases, useRuntime } from '../hooks/useReleases';
import { useTranslation } from '../i18n/context';
import { errorMessage } from '../services/shared/errors';

const COLUMNS = 6;

/**
 * The project's releases in the environment the page is served from: cut
 * them where they were tested, import them into the next environment, and
 * make them live there together.
 */
export function ReleaseList() {
  const { t } = useTranslation();
  const releases = useReleases();
  const runtime = useRuntime();
  const actions = useReleaseActions();
  const [cutting, setCutting] = useState(false);
  const [importing, setImporting] = useState(false);
  const protectedRuntime = runtime.data?.current.protected ?? true;
  const rows = releases.data ?? [];

  const run = async (label: string, work: () => Promise<Release>) => {
    try {
      const release = await work();
      notifications.show({ color: 'green', title: label, message: release.name });
      return true;
    } catch (err) {
      notifications.show({ color: 'red', title: t('releases.failed'), message: errorMessage(err), autoClose: false });
      return false;
    }
  };

  return (
    <PageShell>
      <PageHeader
        title={t('page.releases.title')}
        description={t('page.releases.subtitle')}
        actions={
          <Group gap="xs">
            <Button variant="default" onClick={() => setImporting(true)}>
              {t('releases.import')}
            </Button>
            <Button variant="default" onClick={() => setCutting(true)}>
              {t('releases.cut')}
            </Button>
            {canRollBack(rows) && (
              <Button
                color="orange"
                variant="light"
                loading={actions.rollBack.isPending}
                onClick={() => void run(t('releases.rolledBack'), () => actions.rollBack.mutateAsync())}
              >
                {t('releases.rollBack')}
              </Button>
            )}
          </Group>
        }
      />
      <Card withBorder radius="lg" p={0} mt="lg">
        {releases.isLoading ? (
          <TableLoadingState columns={COLUMNS} />
        ) : releases.error ? (
          <ErrorState error={releases.error} action={t('releases.loadAction')} onRetry={() => void releases.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState icon={PackageCheck} title={t('releases.empty.title')} description={t('releases.empty.description')} />
        ) : (
          <Table verticalSpacing="sm" highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>{t('releases.column.name')}</Table.Th>
                <Table.Th>{t('releases.column.state')}</Table.Th>
                <Table.Th>{t('releases.column.models')}</Table.Th>
                <Table.Th>{t('releases.column.people')}</Table.Th>
                <Table.Th>{t('releases.column.live')}</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((release) => {
                const can = releaseActions(release, protectedRuntime);
                return (
                  <Table.Tr key={release.id}>
                    <Table.Td>
                      <Text fw={600}>{release.name}</Text>
                      <Text size="xs" c="dimmed" ff="monospace">
                        {release.hash.slice(0, 12)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge color={releaseStateColor(release.state)} variant="light">
                        {t(`releases.state.${release.state}`)}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">
                        {release.items.map((item) => `${item.key}${item.version ? ` v${item.version}` : ''}`).join(', ')}
                      </Text>
                      {release.connections.length > 0 && (
                        <Text size="xs" c="dimmed">
                          {t('releases.connections', { names: release.connections.join(', ') })}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs">{t('releases.cutBy', { name: release.cut_by })}</Text>
                      {release.imported_by && <Text size="xs">{t('releases.importedBy', { name: release.imported_by })}</Text>}
                      {release.approved_by && <Text size="xs">{t('releases.approvedBy', { name: release.approved_by })}</Text>}
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{release.live_at ? new Date(release.live_at).toLocaleString() : '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end" wrap="nowrap">
                        {can.approve && (
                          <Button
                            size="xs"
                            variant="light"
                            onClick={() => void run(t('releases.approved'), () => actions.approve.mutateAsync(release.name))}
                          >
                            {t('releases.approve')}
                          </Button>
                        )}
                        {can.goLive && (
                          <Button
                            size="xs"
                            color={protectedRuntime ? 'red' : undefined}
                            loading={actions.goLive.isPending && actions.goLive.variables === release.name}
                            onClick={() => void run(t('releases.wentLive'), () => actions.goLive.mutateAsync(release.name))}
                          >
                            {t('releases.goLive')}
                          </Button>
                        )}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        )}
      </Card>

      <CutDialog
        opened={cutting}
        onClose={() => setCutting(false)}
        onCut={async (name, processes) => {
          if (await run(t('releases.wasCut'), () => actions.cut.mutateAsync({ name, processes, decisions: [] }))) setCutting(false);
        }}
        pending={actions.cut.isPending}
      />
      <ImportDialog
        opened={importing}
        onClose={() => setImporting(false)}
        sources={(runtime.data?.environments ?? []).filter((env) => env.id !== runtime.data?.current.id)}
        includeOwn={Boolean(runtime.data?.current.id)}
        onImport={async (fromEnvironment, name) => {
          if (await run(t('releases.imported'), () => actions.importFrom.mutateAsync({ fromEnvironment, name }))) setImporting(false);
        }}
        pending={actions.importFrom.isPending}
      />
    </PageShell>
  );
}

function CutDialog(props: {
  opened: boolean;
  onClose: () => void;
  onCut: (name: string, processes: string[]) => Promise<void>;
  pending: boolean;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [keys, setKeys] = useState('');
  const processes = parseKeys(keys);
  return (
    <Modal opened={props.opened} onClose={props.onClose} title={t('releases.cut')}>
      <Stack>
        <Text size="sm" c="dimmed">
          {t('releases.cutHelp')}
        </Text>
        <TextInput label={t('releases.name')} placeholder="2026.11.1" value={name} onChange={(e) => setName(e.currentTarget.value)} required />
        <Textarea
          label={t('releases.processes')}
          description={t('releases.processesHelp')}
          placeholder="claims, onboarding"
          value={keys}
          onChange={(e) => setKeys(e.currentTarget.value)}
          autosize
          minRows={2}
        />
        <Group justify="flex-end">
          <Button
            disabled={!name.trim() || processes.length === 0}
            loading={props.pending}
            onClick={() => void props.onCut(name.trim(), processes)}
          >
            {t('releases.cut')}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}

function ImportDialog(props: {
  opened: boolean;
  onClose: () => void;
  sources: Array<{ id: string; name: string; protected: boolean }>;
  includeOwn: boolean;
  onImport: (fromEnvironment: string, name: string) => Promise<void>;
  pending: boolean;
}) {
  const { t } = useTranslation();
  const own = t('releases.ownRuntime');
  const choices = [
    ...(props.includeOwn ? [{ value: OWN_RUNTIME_ID, label: own }] : []),
    ...props.sources.map((env) => ({ value: env.id, label: runtimeLabel(env, own) })),
  ];
  const [from, setFrom] = useState<string | null>(null);
  const [name, setName] = useState('');
  return (
    <Modal opened={props.opened} onClose={props.onClose} title={t('releases.import')}>
      <Stack>
        <Text size="sm" c="dimmed">
          {t('releases.importHelp')}
        </Text>
        <Select label={t('releases.from')} data={choices} value={from} onChange={setFrom} required />
        <TextInput label={t('releases.name')} placeholder="2026.11.1" value={name} onChange={(e) => setName(e.currentTarget.value)} required />
        <Group justify="flex-end">
          <Button
            disabled={!from || !name.trim()}
            loading={props.pending}
            onClick={() => void props.onImport(from === OWN_RUNTIME_ID ? '' : (from ?? ''), name.trim())}
          >
            {t('releases.import')}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
