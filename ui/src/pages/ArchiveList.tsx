import { Button, Card, Group, Table, Text, TextInput, UnstyledButton } from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { Archive } from 'lucide-react';
import { useState } from 'react';

import { ArchivedInstanceDrawer } from '../components/archive/ArchivedInstanceDrawer';
import { formatWhen } from '../components/archive/formatWhen';
import { PageHeader, PageShell } from '../components/layout';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { processLabel, type ArchiveFilters, type ArchivedInstance } from '../domain/archive';
import { useArchive } from '../hooks/useArchive';
import { useTranslation } from '../i18n/context';

const COLUMNS = 5;

/** How long typing pauses before it is sent: one request per pause, not per key. */
const FILTER_PAUSE_MS = 300;

/**
 * The project's finished instances that were archived after their retention
 * period: listed newest finished first, searched by process or business key,
 * and opened read-only. The live instance list no longer shows them, so this
 * is where somebody looking for an old case finds it.
 */
export function ArchiveList() {
  const { t } = useTranslation();
  const [processKey, setProcessKey] = useState('');
  const [businessKey, setBusinessKey] = useState('');
  const [sentProcess] = useDebouncedValue(processKey.trim(), FILTER_PAUSE_MS);
  const [sentBusinessKey] = useDebouncedValue(businessKey.trim(), FILTER_PAUSE_MS);
  const [opened, setOpened] = useState<string | null>(null);

  const filters: ArchiveFilters = { processKey: sentProcess || undefined, businessKey: sentBusinessKey || undefined };
  const isFiltered = Boolean(filters.processKey || filters.businessKey);
  const { projectId, instances, isLoading, error, refetch, hasMore, isLoadingMore, loadMore } = useArchive(filters);

  const clearFilters = () => {
    setProcessKey('');
    setBusinessKey('');
  };

  return (
    <PageShell>
      <PageHeader title={t('page.archive.title')} description={t('page.archive.subtitle')} />
      <Card withBorder radius="lg" p={0}>
        <Group px="md" py="sm" gap="sm" wrap="wrap" align="flex-end">
          <TextInput
            label={t('archive.filter.process')}
            placeholder={t('archive.filter.processPlaceholder')}
            value={processKey}
            onChange={(event) => setProcessKey(event.currentTarget.value)}
            size="xs"
            w={200}
          />
          <TextInput
            label={t('archive.filter.businessKey')}
            placeholder={t('archive.filter.businessKeyPlaceholder')}
            value={businessKey}
            onChange={(event) => setBusinessKey(event.currentTarget.value)}
            size="xs"
            w={200}
          />
          {isFiltered && (
            <Button variant="subtle" color="gray" size="xs" onClick={clearFilters}>
              {t('archive.clearFilters')}
            </Button>
          )}
        </Group>
        {!projectId ? (
          <EmptyState icon={Archive} title={t('archive.noProject.title')} description={t('archive.noProject.description')} />
        ) : error ? (
          <ErrorState error={error} action="load the archive" onRetry={refetch} />
        ) : isLoading ? (
          <TableLoadingState rows={6} columns={COLUMNS} />
        ) : instances.length === 0 ? (
          <EmptyState
            icon={Archive}
            title={isFiltered ? t('archive.empty.filteredTitle') : t('archive.empty.title')}
            description={isFiltered ? t('archive.empty.filteredDescription') : t('archive.empty.description')}
            variant={isFiltered ? 'filtered' : undefined}
          />
        ) : (
          <>
            <ArchiveTable instances={instances} onOpen={setOpened} />
            {hasMore && (
              <Group justify="center" px="md" py="sm">
                <Button variant="default" size="sm" loading={isLoadingMore} onClick={loadMore}>
                  {t('archive.loadMore')}
                </Button>
              </Group>
            )}
          </>
        )}
      </Card>
      <ArchivedInstanceDrawer instanceId={opened} onClose={() => setOpened(null)} />
    </PageShell>
  );
}

/** The rows. Exported for the page test. Each row opens its instance, keyboard included. */
export function ArchiveTable({ instances, onOpen }: { instances: ArchivedInstance[]; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  return (
    <Table.ScrollContainer minWidth={760}>
      <Table verticalSpacing="sm" highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>{t('archive.column.process')}</Table.Th>
            <Table.Th>{t('archive.column.businessKey')}</Table.Th>
            <Table.Th>{t('archive.column.status')}</Table.Th>
            <Table.Th>{t('archive.column.finished')}</Table.Th>
            <Table.Th>{t('archive.column.archived')}</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {instances.map((instance) => (
            <Table.Tr key={instance.id}>
              <Table.Td>
                <UnstyledButton
                  onClick={() => onOpen(instance.id)}
                  aria-label={t('archive.open', { process: processLabel(instance) })}
                >
                  <Text size="sm" fw={500} c="blue">{processLabel(instance)}</Text>
                </UnstyledButton>
              </Table.Td>
              <Table.Td>
                <Text size="sm">{instance.business_key ?? ''}</Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm">{t(`archive.status.${instance.status}`)}</Text>
              </Table.Td>
              <Table.Td style={{ whiteSpace: 'nowrap' }}>
                <Text size="sm" component="time" dateTime={instance.finished_at}>{formatWhen(instance.finished_at)}</Text>
              </Table.Td>
              <Table.Td style={{ whiteSpace: 'nowrap' }}>
                <Text size="sm" component="time" dateTime={instance.archived_at}>{formatWhen(instance.archived_at)}</Text>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
