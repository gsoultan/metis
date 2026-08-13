import {
  Table,
  Card,
  Text,
  Button,
  Group,
  Stack,
  Badge,
  ActionIcon,
  Tooltip,
  Skeleton,
  Center,
  Pagination,
  Select,
} from '@mantine/core';
import { 
  Play, 
  CheckCircle, 
  AlertCircle, 
  Eye, 
  RefreshCw,
} from 'lucide-react';
import { useInstances } from '../hooks/useProcess';
import { PageHeader } from '../components/PageHeader';
import { ErrorState } from '../components/state';
import {useState} from 'react';
import { useDefinitions } from '../hooks/useDefinitions';

/**
 * Turns a node identifier into something readable.
 *
 * Node IDs are authored in the designer and usually carry the step's name in
 * them — "Activity_ApproveExpense", "approve-expense", "Task_1". Splitting the
 * generated prefix and the separators recovers a usable label without needing
 * the whole definition loaded just to render a row.
 */
/** The name of the process an instance belongs to, resolved through its id. */
function definitionName(
  instance: { definition?: { id?: string; key?: string; name?: string } },
  definitions: Array<{ id: string; key?: string; name?: string }>,
): string {
  const fromInstance = instance.definition?.name || instance.definition?.key;
  if (fromInstance) return fromInstance;
  const match = definitions.find((d) => d.id === instance.definition?.id);
  return match?.name || match?.key || 'Process';
}

function humanizeNodeId(nodeId: string): string {
  const withoutPrefix = nodeId.replace(/^(Activity|Task|Event|Gateway|Flow|Node)[_-]/i, '');
  const spaced = withoutPrefix
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .trim();
  if (!spaced || /^\d+$/.test(spaced)) return nodeId;
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

export function InstanceList({ onViewInstance }: { onViewInstance: (instanceId: string, definitionId: string) => void }) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const { data, isLoading, error, refetch } = useInstances(page, pageSize);
  // A listed instance carries only its definition's id, so every row read
  // "Process". The definitions are already a cached query; joining them here
  // costs one more request and gives each row the name of the process it is.
  const { data: definitionsData } = useDefinitions();
  const definitions = definitionsData?.definitions ?? [];
  const pageInfo = data?.pageInfo;

  // Changing the window size invalidates the current offset. Adjusted during
  // render rather than in an effect, which would render once with the old page
  // against the new size and then again to correct it.
  const [appliedPageSize, setAppliedPageSize] = useState(pageSize);
  if (pageSize !== appliedPageSize) {
    setAppliedPageSize(pageSize);
    setPage(1);
  }

  if (isLoading) {
    return (
      <Stack gap="xl">
        <Skeleton height={40} radius="md" />
        <Card withBorder radius="lg" p={0}>
          <Table verticalSpacing="md">
            <thead>
              <tr>
                <th>Instance ID</th>
                <th>Status</th>
                <th>Active Nodes</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 4 }).map((_, i) => (
                <tr key={i}>
                  <td><Skeleton height={16} width="50%" /></td>
                  <td><Skeleton height={16} width={80} /></td>
                  <td><Skeleton height={16} width="40%" /></td>
                  <td><Skeleton height={16} width={60} /></td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      </Stack>
    );
  }

  // A rejected request previously fell through to the empty state, so an
  // outage was reported to the user as "you have nothing".
  if (error) {
    return <ErrorState error={error} action="load your process instances" onRetry={() => refetch()} />;
  }

  const instances = data?.instances || [];

  const getStatusBadge = (status: string) => {
    switch (status.toLowerCase()) {
      case 'active':
        return <Badge color="blue" variant="light" leftSection={<Play size={10} />}>Active</Badge>;
      case 'completed':
        return <Badge color="green" variant="light" leftSection={<CheckCircle size={10} />}>Completed</Badge>;
      case 'failed':
        return <Badge color="red" variant="light" leftSection={<AlertCircle size={10} />}>Failed</Badge>;
      default:
        return <Badge color="gray" variant="light">{status}</Badge>;
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Process Instances" 
        description="Monitor and track the execution of process definitions."
        actions={
          <Button variant="light" leftSection={<RefreshCw size={16} />} onClick={() => refetch()}>Refresh</Button>
        }
      />

      <Card withBorder radius="lg" p={0}>
        <Table verticalSpacing="md">
          <thead>
            <tr>
              <th>Instance ID</th>
              <th>Status</th>
              <th>Active Nodes</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {instances.length === 0 ? (
              <tr>
                <td colSpan={4}>
                  <Center py="xl">
                    <Stack align="center" gap="xs">
                      <Text size="sm" c="dimmed">No instances found for this project</Text>
                    </Stack>
                  </Center>
                </td>
              </tr>
            ) : (
              instances.map((inst) => (
                <tr key={inst.id}>
                  <td>
                    {/* The full UUID was the first column, in bold monospace,
                        as though it were the thing a person identifies an
                        instance by. It is not — the process is. */}
                    <Text size="sm" fw={500}>{definitionName(inst, definitions)}</Text>
                    <Text size="xs" c="dimmed" ff="monospace">{String(inst.id).slice(0, 8)}</Text>
                  </td>
                  <td>{getStatusBadge(inst.status)}</td>
                  <td>
                    {/*
                      This printed the raw node ID — "Activity_1x2y3z" — as the
                      answer to "where is this process right now", which is the
                      single most-asked question about a running instance and
                      the one a machine identifier cannot answer.

                      It also read `active_nodes`; the field arrives from the
                      Connect client as `activeNodes`, so it was always empty
                      and every running instance claimed to have no active
                      steps.
                    */}
                    <Group gap={4}>
                      {(inst.activeNodes ?? []).map((node) => (
                        <Badge key={node.id} size="sm" variant="light" color="blue">
                          {humanizeNodeId(node.id)}
                        </Badge>
                      ))}
                      {(inst.activeNodes ?? []).length === 0 && (
                        <Text size="xs" c="dimmed">
                          {inst.status === 'active' ? 'Starting…' : 'Nothing in progress'}
                        </Text>
                      )}
                    </Group>
                  </td>
                  <td>
                    <Tooltip label="View Execution Path">
                      <ActionIcon aria-label="View instance" 
                        variant="light" 
                        color="blue" 
                        onClick={() => onViewInstance(inst.id, inst.definition?.id ?? '')}
                      >
                        <Eye size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </Table>

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
                data={['25', '50', '100']}
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
    </Stack>
  );
}
