import {
  ActionIcon,
  Badge,
  Button,
  Card,
  Center,
  Group,
  Loader,
  Modal,
  Pagination,
  Select,
  Skeleton,
  Stack,
  Table,
  Text,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useNavigate } from '@tanstack/react-router';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { Eye, GitBranch, History, Network, Play, Plus } from 'lucide-react';
import { lazy, Suspense, useState } from 'react';

import { BPMNGraph } from '../components/BPMNGraph';
import { CreationWizard } from '../components/CreationWizard';
import { PageHeader } from '../components/PageHeader';
/**
 * Loaded on demand, not with the page.
 *
 * The modal pulls in Mantine's date-time picker for scheduling a cutover, which
 * is about 10 kB of the critical path for a dialog that opens on a button
 * press — enough on its own to push first paint over its ceiling.
 */
const VersionHistoryModal = lazy(() =>
  import('../components/VersionHistoryModal').then((m) => ({ default: m.VersionHistoryModal })),
);
import { ErrorState } from '../components/state';
import { versionsByKey } from '../domain/definitionVersions';
import { rowVersions } from '../domain/versionRollout';
// The two dialogs need only enough to name a definition and start it; keeping
// the state to this shape avoids coupling to either the list's or the detail
// endpoint's fuller (and differently-cased) type.
type DefinitionRef = { id: string; key: string; name: string; version: number };
import { useDefinition, useDefinitions, useStartProcess } from '../hooks/useProcess';
import { useLiveVersions } from '../hooks/useDefinitions';
import { errorMessage } from '../services/shared/errors';
import { useTranslation } from '../i18n/context';

dayjs.extend(relativeTime);

const PAGE_SIZES = ['25', '50', '100'];
const COLUMNS = 5;

export function DefinitionList({ onEditModel, hideHeader }: { onEditModel?: (id: string) => void, hideHeader?: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const { data, isLoading, error, refetch } = useDefinitions(page, pageSize);
  const startProcess = useStartProcess();
  const { data: liveData } = useLiveVersions();
  const liveVersions = liveData?.live ?? {};
  const [selectedDef, setSelectedDef] = useState<DefinitionRef | null>(null);
  const [historyKey, setHistoryKey] = useState<string | null>(null);
  const [wizardOpened, setWizardOpened] = useState(false);
  // The process waiting for "yes, start it", and the one being started.
  const [pendingRun, setPendingRun] = useState<DefinitionRef | null>(null);
  const [startingKey, setStartingKey] = useState<string | null>(null);

  const { data: fullDefData, isLoading: isFullLoading } = useDefinition(selectedDef?.id || null);

  const [appliedPageSize, setAppliedPageSize] = useState(pageSize);
  if (pageSize !== appliedPageSize) {
    setAppliedPageSize(pageSize);
    setPage(1);
  }

  const definitions = data?.definitions ?? [];
  const pageInfo = data?.pageInfo;
  // One row per key, showing the version that is actually live rather than the
  // highest one deployed. Those are the same thing until somebody stages a
  // version, and different in the way that matters after — the newest version
  // may be one no instance will ever start on.
  const rows = Object.values(versionsByKey(definitions))
    .map((group) => rowVersions(group, liveVersions))
    .filter((row) => row !== null);
  const latestDefinitions = rows.map((row) => row.live);

  /** The version waiting behind the live one for this key, if any. */
  const stagedFor = (key: string) =>
    rows.find((row) => row.live.key === key)?.staged?.version ?? null;

  const startNow = async (def: DefinitionRef) => {
    setPendingRun(null);
    setStartingKey(def.key);
    try {
      // startProcess raises on a refusal, so the catch below is the one path.
      await startProcess.mutateAsync({ definitionKey: def.key });
      notifications.show({ title: `Started ${def.name}`, message: 'Follow it under Instances.', color: 'green' });
    } catch (err: unknown) {
      notifications.show({ title: `Could not start ${def.name}`, message: errorMessage(err, 'It did not start.'), color: 'red' });
    } finally {
      setStartingKey(null);
    }
  };

  if (isLoading) {
    return (
      <Stack gap="xl">
        <Skeleton height={40} radius="md" />
        <Card shadow="sm" radius="lg" withBorder p={0}>
          <Table.ScrollContainer minWidth={800}>
            <Table verticalSpacing="md" horizontalSpacing="xl">
              <Table.Thead bg="gray.0"><HeaderRow /></Table.Thead>
              <Table.Tbody>
                {Array.from({ length: 4 }).map((_, i) => (
                  <Table.Tr key={i}>
                    <Table.Td><Skeleton height={16} width="60%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width="40%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width={40} /></Table.Td>
                    <Table.Td><Skeleton height={16} width="50%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width={100} ml="auto" /></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Card>
      </Stack>
    );
  }

  // A rejected request previously fell through to the empty state, so an
  // outage was reported to the user as "you have nothing".
  if (error) {
    return <ErrorState error={error} action="load your process models" onRetry={() => refetch()} />;
  }


  return (
    <Stack gap="xl">
      {!hideHeader && (
        <PageHeader
          title={t('page.definitions.title')}
          description={t('page.definitions.subtitle')}
          actions={
            <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => setWizardOpened(true)}>
              Create New
            </Button>
          }
        />
      )}

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0"><HeaderRow /></Table.Thead>
            <Table.Tbody>
              {latestDefinitions.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    <Stack align="center" py={60} gap="sm">
                      <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                        <GitBranch size={32} />
                      </ThemeIcon>
                      <Text fw={700} size="lg">No models found</Text>
                      <Text ta="center" c="dimmed" maw={400}>
                        Start by creating your first process definition to automate your workflow.
                      </Text>
                      <Button variant="subtle" mt="md" onClick={() => setWizardOpened(true)}>Create your first process</Button>
                    </Stack>
                  </Table.Td>
                </Table.Tr>
              ) : (
                latestDefinitions.map((def) => (
                  <Table.Tr key={def.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <Network size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{def.name}</Text>
                          {def.documentation && <Text size="xs" c="dimmed" lineClamp={1}>{def.documentation}</Text>}
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="outline" color="gray" radius="sm" styles={{ label: { textTransform: 'none' } }}>{def.key}</Badge>
                    </Table.Td>
                    <Table.Td>
                      {/*
                        These read "v1 live" and "v4 staged", and the column
                        clipped them to "V1 LI…" and "V4 STA…" — Mantine
                        uppercases a Badge label and ellipsises it on overflow,
                        and two of them in a nowrap Group shrink rather than
                        wrap. Which version is live is the single most important
                        fact about a deployed process, so it cannot be the thing
                        the layout drops first.
                      */}
                      <Group gap={6} wrap="wrap">
                        <Tooltip label="New instances start on this version">
                          <Badge
                            variant="light"
                            color="blue"
                            styles={{ label: { textTransform: 'none', overflow: 'visible' } }}
                          >
                            v{def.version} live
                          </Badge>
                        </Tooltip>
                        {stagedFor(def.key) !== null && (
                          <Tooltip label={`v${stagedFor(def.key)} is deployed but takes no work yet. Promote it from Version history.`}>
                            <Badge
                              variant="outline"
                              color="orange"
                              size="sm"
                              styles={{ label: { textTransform: 'none', overflow: 'visible' } }}
                            >
                              v{stagedFor(def.key)} staged
                            </Badge>
                          </Tooltip>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs" c="dimmed">{dayjs(def.createdAt).fromNow()}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Button
                          size="xs"
                          variant="light"
                          color="green"
                          leftSection={<Play size={14} />}
                          onClick={() => setPendingRun(def)}
                          loading={startingKey === def.key}
                        >
                          Run
                        </Button>
                        <Tooltip label="Version history">
                          <ActionIcon aria-label={`Version history of ${def.name}`} variant="light" color="orange" onClick={() => setHistoryKey(def.key)}>
                            <History size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Edit flow">
                          <ActionIcon aria-label={`Edit ${def.name}`} variant="light" color="blue" onClick={() => onEditModel?.(def.id)}>
                            <GitBranch size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="View diagram">
                          <ActionIcon aria-label={`View ${def.name}`} variant="light" color="indigo" onClick={() => setSelectedDef(def)}>
                            <Eye size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>

        {/* Only the first page used to exist: the hook was paged, the page
            never read pageInfo, and a project's 26th process was unreachable. */}
        {pageInfo && pageInfo.total > pageInfo.pageSize && (
          <Group justify="space-between" px="md" py="sm" wrap="wrap" gap="sm">
            <Text size="sm" c="dimmed">
              {`${(pageInfo.page - 1) * pageInfo.pageSize + 1}–` +
                `${Math.min(pageInfo.page * pageInfo.pageSize, pageInfo.total)}` +
                ` of ${pageInfo.total.toLocaleString()} versions`}
            </Text>
            <Group gap="sm" wrap="nowrap">
              <Select
                aria-label="Versions per page"
                data={PAGE_SIZES}
                value={String(pageSize)}
                onChange={(value) => value && setPageSize(Number(value))}
                size="xs"
                w={92}
                allowDeselect={false}
                comboboxProps={{ withinPortal: true }}
              />
              <Pagination
                value={pageInfo.page}
                onChange={setPage}
                total={Math.max(1, Math.ceil(pageInfo.total / pageInfo.pageSize))}
                size="sm"
                withEdges
                getControlProps={(control) => ({
                  'aria-label': {
                    first: 'First page',
                    last: 'Last page',
                    next: 'Next page',
                    previous: 'Previous page',
                  }[control] ?? undefined,
                })}
              />
            </Group>
          </Group>
        )}
      </Card>

      <Modal
        opened={pendingRun !== null}
        onClose={() => setPendingRun(null)}
        title={<Text fw={700}>Start {pendingRun?.name}?</Text>}
        radius="lg"
      >
        <Stack gap="md">
          <Text size="sm">
            A new instance of version {pendingRun?.version} starts now. Its first steps run immediately, and anyone
            given a task in it is notified.
          </Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setPendingRun(null)}>Cancel</Button>
            <Button color="green" leftSection={<Play size={14} />} onClick={() => pendingRun && startNow(pendingRun)}>
              Start
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/*
        The history is fetched rather than derived from the list page: which
        version is live, and how many instances each one is still finishing, are
        facts only the server has. Grouping the list client-side could show the
        version numbers but never the state that matters.
      */}
      {historyKey !== null && (
        <Suspense fallback={null}>
          <VersionHistoryModal
            processKey={historyKey}
            onClose={() => setHistoryKey(null)}
            onView={(version) => { setSelectedDef(version); setHistoryKey(null); }}
          />
        </Suspense>
      )}

      <Modal
        opened={!!selectedDef}
        onClose={() => setSelectedDef(null)}
        title={<Text fw={700}>{selectedDef?.name} (v{selectedDef?.version})</Text>}
        size="xl"
        radius="lg"
      >
        {isFullLoading ? (
          <Center py="xl"><Loader /></Center>
        ) : !!fullDefData?.definition && (
          <Stack gap="md">
            <BPMNGraph nodes={fullDefData.definition.nodes} flows={fullDefData.definition.flows} />
            <Group justify="flex-end">
              <Button onClick={() => setSelectedDef(null)}>Close</Button>
              <Button
                variant="filled"
                color="green"
                leftSection={<Play size={16} />}
                onClick={() => {
                  const d = fullDefData.definition;
                  setPendingRun(d ? { id: d.id, key: d.key, name: d.name, version: d.version } : null);
                  setSelectedDef(null);
                }}
              >
                Run
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>

      <CreationWizard
        opened={wizardOpened}
        onClose={() => setWizardOpened(false)}
        initialType="process"
        onCreateProcess={(data) => navigate({ to: '/designer', search: { name: data.name, key: data.key } })}
        onCreateDecision={(data) => navigate({ to: '/decision-editor', search: { name: data.name, key: data.key } })}
      />
    </Stack>
  );
}

function HeaderRow() {
  return (
    <Table.Tr>
      <Table.Th>Process</Table.Th>
      <Table.Th>
        <Tooltip label="What other processes and the API call it by">
          <span>Reference</span>
        </Tooltip>
      </Table.Th>
      <Table.Th>Version</Table.Th>
      <Table.Th>Deployed</Table.Th>
      <Table.Th ta="right">Actions</Table.Th>
    </Table.Tr>
  );
}
