import { Anchor, Button, Card, Group, Select, Table, Text, TextInput, Tooltip } from '@mantine/core';
// Imported here rather than at the root, as TaskForm explains: the picker's
// stylesheet belongs with the picker, not on every visitor's first paint.
import '@mantine/dates/styles.css';
import { DateInput } from '@mantine/dates';
import { useDebouncedValue } from '@mantine/hooks';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { RefreshCw, ScrollText, ShieldCheck } from 'lucide-react';
import { useState } from 'react';

import { PageHeader, PageShell } from '../components/layout';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { hasRole } from '../domain/access';
import {
  AUDIT_EVENT_TYPES,
  auditTypeLabel,
  endOfLocalDay,
  shortId,
  startOfLocalDay,
  type AuditFilters,
  type AuditOperation,
} from '../domain/auditOperations';
import { PRIVILEGED_ROLE } from '../domain/roles';
import { useAuditOperations } from '../hooks/useAuditOperations';
import { useProjects } from '../hooks/useProcess';
import { useViewerAccess } from '../hooks/useViewerAccess';
import { useTranslation } from '../i18n/context';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

const COLUMNS = 5;

/** How long typing a name pauses before it is sent: one request per pause, not per key. */
const ACTOR_PAUSE_MS = 300;

/**
 * Who did what across the organization: every waive, migration, reassignment,
 * cancel, hold and approval, newest first.
 *
 * The page answers an administrator's "who touched this, and when?" — so the
 * sentence the server wrote is the row's main content, and the time is
 * absolute in the reader's own clock rather than "3 days ago", which cannot be
 * quoted back in an incident report. Older operations are read a page at a
 * time behind the server's cursor; changing any filter asks a new question and
 * starts again from the newest.
 */
export function AuditLog() {
  const { t } = useTranslation();
  const viewer = useViewerAccess();
  const isAdministrator = hasRole(viewer, PRIVILEGED_ROLE);

  if (!isAdministrator) {
    return (
      <PageShell>
        <PageHeader title={t('page.audit.title')} description={t('page.audit.subtitle')} />
        <EmptyState
          icon={ShieldCheck}
          title={t('audit.adminOnly.title')}
          description={t('audit.adminOnly.description')}
        />
      </PageShell>
    );
  }
  return <AuditLogTable />;
}

function AuditLogTable() {
  const { t } = useTranslation();
  const [actor, setActor] = useState('');
  const [sentActor] = useDebouncedValue(actor.trim(), ACTOR_PAUSE_MS);
  const [type, setType] = useState<string | null>(null);
  const [project, setProject] = useState<string | null>(null);
  const [fromDay, setFromDay] = useState<string | null>(null);
  const [toDay, setToDay] = useState<string | null>(null);

  // Derived each render; the query key holds it, so a change starts the list
  // again from the newest operation.
  const filters: AuditFilters = {
    actor: sentActor || undefined,
    type: type ?? undefined,
    project: project ?? undefined,
    from: startOfLocalDay(fromDay),
    to: endOfLocalDay(toDay),
  };
  const isFiltered = Object.values(filters).some((value) => value !== undefined);

  const { operations, isLoading, isFetching, error, refetch, hasMore, isLoadingMore, loadMore } =
    useAuditOperations(filters);

  const currentOrganizationId = useAppStore((state) => state.currentOrganizationId);
  const { data: projectsData } = useProjects(currentOrganizationId);
  const projects = projectsData?.projects ?? [];
  const projectNames = new Map(projects.map((p) => [p.id, p.name]));

  const typeOptions = AUDIT_EVENT_TYPES.map((value) => ({ value, label: auditTypeLabel(value, t) }));
  const projectOptions = projects.map((p) => ({ value: p.id, label: p.name || shortId(p.id) }));

  const clearFilters = () => {
    setActor('');
    setType(null);
    setProject(null);
    setFromDay(null);
    setToDay(null);
  };

  return (
    <PageShell>
      <PageHeader
        title={t('page.audit.title')}
        description={t('page.audit.subtitle')}
        actions={
          <Button variant="light" leftSection={<RefreshCw size={16} />} loading={isFetching} onClick={refetch}>
            {t('audit.refresh')}
          </Button>
        }
      />

      <Card withBorder radius="lg" p={0}>
        <Group px="md" py="sm" gap="sm" wrap="wrap" align="flex-end">
          <TextInput
            label={t('audit.filter.actor')}
            placeholder={t('audit.filter.actorPlaceholder')}
            value={actor}
            onChange={(event) => setActor(event.currentTarget.value)}
            size="xs"
            w={180}
          />
          <Select
            label={t('audit.filter.type')}
            placeholder={t('audit.filter.allTypes')}
            data={typeOptions}
            value={type}
            onChange={setType}
            clearable
            searchable
            size="xs"
            w={220}
            comboboxProps={{ withinPortal: true }}
          />
          {projectOptions.length > 0 && (
            <Select
              label={t('audit.filter.project')}
              placeholder={t('audit.filter.allProjects')}
              data={projectOptions}
              value={project}
              onChange={setProject}
              clearable
              searchable
              size="xs"
              w={200}
              comboboxProps={{ withinPortal: true }}
            />
          )}
          <DateInput
            label={t('audit.filter.from')}
            value={fromDay}
            onChange={(value) => setFromDay(value ? String(value) : null)}
            maxDate={toDay ?? undefined}
            valueFormat="YYYY-MM-DD"
            clearable
            size="xs"
            w={140}
          />
          <DateInput
            label={t('audit.filter.to')}
            value={toDay}
            onChange={(value) => setToDay(value ? String(value) : null)}
            minDate={fromDay ?? undefined}
            valueFormat="YYYY-MM-DD"
            clearable
            size="xs"
            w={140}
          />
          {isFiltered && (
            <Button variant="subtle" color="gray" size="xs" onClick={clearFilters}>
              {t('audit.clearFilters')}
            </Button>
          )}
        </Group>

        {error ? (
          <ErrorState error={error} action="load the audit log" onRetry={refetch} />
        ) : isLoading ? (
          <TableLoadingState rows={6} columns={COLUMNS} />
        ) : operations.length === 0 ? (
          isFiltered ? (
            <EmptyState
              icon={ScrollText}
              title={t('audit.empty.filteredTitle')}
              description={t('audit.empty.filteredDescription')}
              variant="filtered"
              action={
                <Button variant="light" onClick={clearFilters}>
                  {t('audit.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={ScrollText}
              title={t('audit.empty.title')}
              description={t('audit.empty.description')}
            />
          )
        ) : (
          <>
            <AuditTable operations={operations} projectNames={projectNames} />
            {hasMore && (
              <Group justify="center" px="md" py="sm">
                <Button variant="default" size="sm" loading={isLoadingMore} onClick={loadMore}>
                  {t('audit.loadMore')}
                </Button>
              </Group>
            )}
          </>
        )}
      </Card>
    </PageShell>
  );
}

/** The rows. Exported for the page test, which draws it without the filters around it. */
export function AuditTable({
  operations,
  projectNames,
}: {
  operations: AuditOperation[];
  projectNames: Map<string, string>;
}) {
  const { t } = useTranslation();
  return (
    <Table.ScrollContainer minWidth={860}>
      <Table verticalSpacing="sm" highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>{t('audit.column.when')}</Table.Th>
            <Table.Th>{t('audit.column.who')}</Table.Th>
            <Table.Th>{t('audit.column.what')}</Table.Th>
            <Table.Th>{t('audit.column.narrative')}</Table.Th>
            <Table.Th>{t('audit.column.instance')}</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {operations.map((operation) => (
            <Table.Tr key={operation.id}>
              <Table.Td style={{ whiteSpace: 'nowrap' }}>
                <Text size="sm" component="time" dateTime={operation.at}>
                  {formatWhen(operation.at)}
                </Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm" fw={500}>{operation.actor}</Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm">{auditTypeLabel(operation.type, t)}</Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm">{operation.narrative}</Text>
                {projectNames.has(operation.project_id) && (
                  <Text size="xs" c="dimmed">{projectNames.get(operation.project_id)}</Text>
                )}
              </Table.Td>
              <Table.Td>
                {operation.instance_id ? <InstanceLink instanceId={operation.instance_id} /> : null}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

/** Absolute, in the reader's own clock and locale, to the second. */
function formatWhen(at: string): string {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return at;
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

/**
 * The instance an operation touched, opened in the designer's instance view.
 *
 * That view needs the instance's definition as well as its id, and an
 * operation carries only the id, so the instance is read when somebody asks
 * to open it — from the cache the designer will use anyway — rather than once
 * per row on every page of the log.
 */
function InstanceLink({ instanceId }: { instanceId: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [opening, setOpening] = useState(false);

  const open = async () => {
    setOpening(true);
    let definitionId: string | undefined;
    try {
      const result = await queryClient.fetchQuery({
        queryKey: ['instance', instanceId],
        queryFn: ({ signal }) => processService.getInstance(instanceId, signal),
      });
      definitionId = result.instance?.definition?.id || undefined;
    } catch {
      // Opened without its definition: the instance view still says what it
      // can, and the reason it could not read the rest.
    } finally {
      setOpening(false);
    }
    void navigate({ to: '/designer', search: { instanceId, definitionId } });
  };

  return (
    <Tooltip label={t('audit.openInstance')} openDelay={400}>
      <Anchor
        component="button"
        type="button"
        size="sm"
        ff="monospace"
        onClick={() => void open()}
        disabled={opening}
        aria-label={t('audit.openInstanceNamed', { id: shortId(instanceId) })}
      >
        {shortId(instanceId)}
      </Anchor>
    </Tooltip>
  );
}
