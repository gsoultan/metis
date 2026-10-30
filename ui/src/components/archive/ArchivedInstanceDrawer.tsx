import { Badge, Drawer, Group, Stack, Table, Tabs, Text } from '@mantine/core';

import { processLabel, timelineEntriesOf, valueText, type ArchiveRecord, type ArchivedInstance } from '../../domain/archive';
import { useArchivedInstance } from '../../hooks/useArchive';
import { useTranslation } from '../../i18n/context';
import { TimelineEntries } from '../BusinessTimeline';
import { ErrorState } from '../state';
import { formatWhen } from './formatWhen';

/**
 * One archived instance, read-only: what it was, what happened to it, the work
 * it handed out and the values it finished with. Nothing here acts on it — an
 * archived instance is a record.
 */
export function ArchivedInstanceDrawer({ instanceId, onClose }: { instanceId: string | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { data, isLoading, error, refetch } = useArchivedInstance(instanceId);
  return (
    <Drawer
      opened={instanceId !== null}
      onClose={onClose}
      position="right"
      size="xl"
      title={data ? processLabel(data.instance) : t('archive.drawer.title')}
    >
      {error ? (
        <ErrorState error={error} action="open the archived instance" onRetry={() => void refetch()} />
      ) : isLoading || !data ? (
        <Text c="dimmed">{t('archive.drawer.loading')}</Text>
      ) : (
        <ArchivedInstanceDetail instance={data.instance} record={data.record} />
      )}
    </Drawer>
  );
}

function ArchivedInstanceDetail({ instance, record }: { instance: ArchivedInstance; record: ArchiveRecord }) {
  const { t } = useTranslation();
  const variables = Object.entries(record.instance.variables ?? {});
  return (
    <Stack gap="md">
      <Group gap="xs">
        <Badge variant="light">{t(`archive.status.${instance.status}`)}</Badge>
        {instance.business_key && <Badge variant="outline">{instance.business_key}</Badge>}
      </Group>
      <Text size="sm" c="dimmed">
        {t('archive.drawer.when', {
          started: formatWhen(instance.started_at),
          finished: formatWhen(instance.finished_at),
          archived: formatWhen(instance.archived_at),
        })}
      </Text>
      <Tabs defaultValue="timeline">
        <Tabs.List>
          <Tabs.Tab value="timeline">{t('archive.tab.timeline')}</Tabs.Tab>
          <Tabs.Tab value="tasks">{t('archive.tab.tasks', { count: record.tasks?.length ?? 0 })}</Tabs.Tab>
          <Tabs.Tab value="variables">{t('archive.tab.variables', { count: variables.length })}</Tabs.Tab>
          <Tabs.Tab value="incidents">{t('archive.tab.incidents', { count: record.incidents?.length ?? 0 })}</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="timeline" pt="sm">
          <TimelineEntries entries={timelineEntriesOf(record.audit_trail)} />
        </Tabs.Panel>
        <Tabs.Panel value="tasks" pt="sm">
          <RecordTable
            empty={t('archive.tasks.empty')}
            head={[t('archive.tasks.name'), t('archive.tasks.status'), t('archive.tasks.assignee'), t('archive.tasks.changed')]}
            rows={(record.tasks ?? []).map((task) => [task.id, task.name || task.node_id, task.status, task.assignee ?? '', formatWhen(task.updated_at)])}
          />
        </Tabs.Panel>
        <Tabs.Panel value="variables" pt="sm">
          <RecordTable
            empty={t('archive.variables.empty')}
            head={[t('archive.variables.name'), t('archive.variables.value')]}
            rows={variables.map(([name, value]) => [name, name, valueText(value)])}
          />
        </Tabs.Panel>
        <Tabs.Panel value="incidents" pt="sm">
          <RecordTable
            empty={t('archive.incidents.empty')}
            head={[t('archive.incidents.step'), t('archive.incidents.error'), t('archive.incidents.status')]}
            rows={(record.incidents ?? []).map((incident) => [incident.id, incident.node_id, incident.error, incident.status])}
          />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}

/** A table of plain text rows; each row's first cell is its key, not shown. */
function RecordTable({ head, rows, empty }: { head: string[]; rows: string[][]; empty: string }) {
  if (rows.length === 0) return <Text c="dimmed" size="sm">{empty}</Text>;
  return (
    <Table.ScrollContainer minWidth={480}>
      <Table verticalSpacing="xs">
        <Table.Thead>
          <Table.Tr>
            {head.map((label) => (
              <Table.Th key={label}>{label}</Table.Th>
            ))}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map(([key, ...cells]) => (
            <Table.Tr key={key}>
              {cells.map((cell, index) => (
                <Table.Td key={index}>
                  <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{cell}</Text>
                </Table.Td>
              ))}
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
