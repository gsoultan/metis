import {
  ActionIcon,
  Badge,
  Button,
  Card,
  Drawer,
  Group,
  Pagination,
  Select,
  Text,
  Tooltip,
  VisuallyHidden,
} from '@mantine/core';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { Pause, Play, RefreshCw, Workflow } from 'lucide-react';
import { useState } from 'react';

import { IncidentInbox } from '../components/IncidentInbox';
import { PageShell, PageHeader } from '../components/layout';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { InstanceTable } from '../components/instances/InstanceTable';
import { StatusChips } from '../components/instances/StatusChips';
import { statusChips, totalAcrossStatuses } from '../domain/instanceList';
import { useDefinitions } from '../hooks/useDefinitions';
import { useInstances } from '../hooks/useProcess';
import { useTranslation } from '../i18n/context';

dayjs.extend(relativeTime);

const PAGE_SIZES = ['25', '50', '100'];
const COLUMNS = 5;

/**
 * Every run of a process in this project, and where each one is.
 *
 * The page exists to answer one question before any other — *is anything
 * broken?* — and it used to answer it badly. Its status filter narrowed the
 * twenty-five rows that had already arrived, so a project with half a million
 * instances and twelve failures offered no way to reach those twelve and no
 * hint that they existed; the list simply looked healthy. The filter now runs
 * in the database, and the counts beside it describe the whole project, so the
 * first thing on screen is the true answer and the control that acts on it.
 *
 * Three other things follow from treating this as an operator's screen rather
 * than a table of records:
 *
 *  - **It is live.** These rows change without anybody touching the page. One
 *    that only updated on a button press was a screenshot of the engine, and
 *    nothing said how old it was.
 *  - **The row is the target.** Opening an instance was a 26px icon; it is now
 *    the whole row, keyboard included, with the icon kept as the visible
 *    affordance rather than as the only one.
 *  - **A control that can only say no is not shown.** "What went wrong" sat on
 *    every row, grey, and answered "nothing has failed on this process" for the
 *    healthy majority who pressed it.
 */
export function InstanceList({ onViewInstance }: { onViewInstance: (instanceId: string, definitionId: string) => void }) {
  const { t } = useTranslation();
  // Which instance's failures are on screen, if any.
  const [inspecting, setInspecting] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [status, setStatus] = useState<string | undefined>(undefined);
  const [definitionId, setDefinitionId] = useState<string | undefined>(undefined);
  const [needsAttention, setNeedsAttention] = useState(false);
  // Polling is worth pausing while somebody reads a row: the list is ordered
  // newest-first, so a process starting underneath them moves everything down.
  const [live, setLive] = useState(true);

  const { data, isLoading, isFetching, error, refetch, dataUpdatedAt } = useInstances(
    page,
    pageSize,
    { status, definitionId, needsAttention },
    live,
  );
  // A listed instance carries only its definition's id, so every row read
  // "Process". The definitions are already a cached query; joining them here
  // costs one more request and gives each row the name of the process it is —
  // and gives this page the list it filters by.
  const { data: definitionsData } = useDefinitions();
  const definitions = definitionsData?.definitions ?? [];
  const pageInfo = data?.pageInfo;

  // Changing the window size, the state or the process invalidates the current
  // offset. Adjusted during render rather than in an effect, which would render
  // once against the old page and then again to correct it.
  const [appliedScope, setAppliedScope] = useState({ pageSize, status, definitionId, needsAttention });
  if (
    pageSize !== appliedScope.pageSize ||
    status !== appliedScope.status ||
    definitionId !== appliedScope.definitionId ||
    needsAttention !== appliedScope.needsAttention
  ) {
    setAppliedScope({ pageSize, status, definitionId, needsAttention });
    setPage(1);
  }

  // A rejected request previously fell through to the empty state, so an
  // outage was reported to the user as "you have nothing".
  if (error) {
    return <ErrorState error={error} action="load your process instances" onRetry={() => refetch()} />;
  }

  const instances = data?.instances ?? [];
  const chips = statusChips(data?.statusCounts ?? []);
  const projectTotal = totalAcrossStatuses(data?.statusCounts ?? []);
  const isFiltered = status !== undefined || definitionId !== undefined || needsAttention;
  // A Set because the table asks it once per row; an array would make drawing a
  // page of 100 quadratic in the number of failures.
  const attentionIds = new Set(data?.needsAttentionIds ?? []);

  const definitionOptions = definitions.map((definition) => ({
    value: definition.id,
    label: definition.name || definition.key || 'Process',
  }));

  const clearFilters = () => {
    setStatus(undefined);
    setDefinitionId(undefined);
    setNeedsAttention(false);
  };

  return (
    <PageShell>
      <PageHeader
        title={t('page.instances.title')}
        description={t('page.instances.subtitle')}
        meta={
          <LiveIndicator live={live} isFetching={isFetching} updatedAt={dataUpdatedAt} />
        }
        actions={
          <Group gap="xs" wrap="nowrap">
            <Tooltip label={live ? 'Stop updating while you read' : 'Update on its own again'}>
              <ActionIcon
                variant="default"
                size="lg"
                aria-label={live ? 'Pause live updates' : 'Resume live updates'}
                onClick={() => setLive((on) => !on)}
              >
                {live ? <Pause size={16} /> : <Play size={16} />}
              </ActionIcon>
            </Tooltip>
            <Button
              variant="light"
              leftSection={<RefreshCw size={16} />}
              loading={isFetching}
              onClick={() => refetch()}
            >
              Refresh
            </Button>
          </Group>
        }
      />

      {/*
        Both the summary and the filter, deliberately one control. The numbers
        are the project's, not the page's, so "12 need attention" stays true
        while twenty-five completed runs are on screen — and the thing that
        makes it true is also the thing that reaches it.
      */}
      {!isLoading && chips.length > 0 && (
        <StatusChips
          counts={chips}
          total={projectTotal}
          selected={status}
          // The two narrowings are one choice on screen, so picking either
          // clears the other. Both at once is a question nobody asked — "failed
          // instances that also need attention" reads as a filter that has
          // stopped meaning anything.
          onSelect={(next) => { setStatus(next); setNeedsAttention(false); }}
          needsAttentionTotal={data?.needsAttentionTotal ?? 0}
          needsAttentionSelected={needsAttention}
          onSelectNeedsAttention={(on) => { setNeedsAttention(on); setStatus(undefined); }}
        />
      )}

      <Card withBorder radius="lg" p={0}>
        <Group px="md" py="sm" justify="space-between" wrap="wrap" gap="sm">
          <Group gap="sm" wrap="nowrap">
            <Select
              aria-label="Show only one process"
              placeholder="Every process"
              data={definitionOptions}
              value={definitionId ?? null}
              onChange={(value) => setDefinitionId(value ?? undefined)}
              clearable
              searchable
              size="xs"
              w={240}
              comboboxProps={{ withinPortal: true }}
            />
            {isFiltered && (
              <Button variant="subtle" color="gray" size="xs" onClick={clearFilters}>
                Clear filters
              </Button>
            )}
          </Group>
          {pageInfo && (
            <Text size="xs" c="dimmed">
              {/* States what the count is *of*, because with a filter on it is
                  no longer the project's total and would otherwise read as one. */}
              {isFiltered
                ? `${pageInfo.total.toLocaleString()} matching`
                : `${pageInfo.total.toLocaleString()} in this project`}
            </Text>
          )}
        </Group>

        {isLoading ? (
          <TableLoadingState rows={6} columns={COLUMNS} />
        ) : instances.length === 0 ? (
          isFiltered ? (
            <EmptyState
              icon={Workflow}
              title="Nothing matches these filters"
              description="No instance in this project matches. Clear the filters to see the rest."
              variant="filtered"
              action={
                <Button variant="light" onClick={clearFilters}>
                  Clear filters
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={Workflow}
              title="No process instances yet"
              description="An instance appears here the moment somebody starts a process — from the designer, the API, or a message arriving. Each one is a run you can follow step by step."
            />
          )
        ) : (
          <InstanceTable
            instances={instances}
            definitions={definitions}
            asOf={dataUpdatedAt}
            needsAttention={attentionIds}
            onOpen={(instance) => onViewInstance(instance.id, instance.definition?.id ?? '')}
            onInspect={setInspecting}
          />
        )}

        {/*
          Shown only when there is more than one page: controls that can never
          do anything are noise. The range is stated in words because "51–75 of
          1,240" answers both questions someone has about a long list, where a
          bare page number answers neither.
        */}
        {pageInfo && pageInfo.total > pageInfo.pageSize && (
          <Group justify="space-between" px="md" py="sm" wrap="wrap" gap="sm">
            <Text size="sm" c="dimmed">
              {`${(pageInfo.page - 1) * pageInfo.pageSize + 1}–` +
                `${Math.min(pageInfo.page * pageInfo.pageSize, pageInfo.total)}` +
                ` of ${pageInfo.total.toLocaleString()}`}
            </Text>
            <Group gap="sm" wrap="nowrap">
              <Select
                aria-label="Instances per page"
                data={PAGE_SIZES}
                value={String(pageSize)}
                onChange={(value) => value && setPageSize(Number(value))}
                size="xs"
                w={92}
                allowDeselect={false}
                comboboxProps={{ withinPortal: true }}
              />
              <Pagination
                // The server reports the page it served, after clamping; using
                // the requested value would let the highlight disagree with
                // what is on screen.
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

      {/* The failures drawer was mounted only while the page was loading, so
          the button that opens it did nothing once there were rows to click. */}
      <Drawer
        opened={inspecting !== null}
        onClose={() => setInspecting(null)}
        position="right"
        size="lg"
        title="What went wrong"
      >
        {inspecting && <IncidentInbox instanceId={inspecting} />}
      </Drawer>
    </PageShell>
  );
}

/**
 * How old what you are looking at is.
 *
 * A live table with nothing saying so is worse than a static one: somebody who
 * does not know it refreshes will not trust it, and somebody who assumes it
 * does will trust a stale screen. This says which of the two is happening.
 */
function LiveIndicator({ live, isFetching, updatedAt }: { live: boolean; isFetching: boolean; updatedAt: number }) {
  if (!updatedAt) return null;
  return (
    <Badge
      variant="light"
      color={live ? 'green' : 'gray'}
      size="sm"
      radius="sm"
      leftSection={
        <span
          aria-hidden
          style={{
            display: 'block',
            width: 7,
            height: 7,
            borderRadius: '50%',
            background: 'currentColor',
            opacity: isFetching ? 0.35 : 1,
            transition: 'opacity 150ms ease',
          }}
        />
      }
      // Polite rather than assertive: the count changing under somebody is not
      // worth interrupting whatever their screen reader is in the middle of.
      aria-live="polite"
    >
      {live ? 'Live' : 'Paused'}
      <VisuallyHidden> — last updated {dayjs(updatedAt).fromNow()}</VisuallyHidden>
    </Badge>
  );
}
