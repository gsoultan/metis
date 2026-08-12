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
  Center
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

/**
 * Turns a node identifier into something readable.
 *
 * Node IDs are authored in the designer and usually carry the step's name in
 * them — "Activity_ApproveExpense", "approve-expense", "Task_1". Splitting the
 * generated prefix and the separators recovers a usable label without needing
 * the whole definition loaded just to render a row.
 */
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
  const { data, isLoading, error, refetch } = useInstances();

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
              instances.map((inst: any) => (
                <tr key={inst.id}>
                  <td>
                    {/* The full UUID was the first column, in bold monospace,
                        as though it were the thing a person identifies an
                        instance by. It is not — the process is. */}
                    <Text size="sm" fw={500}>{inst.definition_key || 'Process'}</Text>
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
                      {(inst.activeNodes ?? inst.active_nodes ?? []).map((nodeId: string) => (
                        <Badge key={nodeId} size="sm" variant="light" color="blue">
                          {humanizeNodeId(nodeId)}
                        </Badge>
                      ))}
                      {(inst.activeNodes ?? inst.active_nodes ?? []).length === 0 && (
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
                        onClick={() => onViewInstance(inst.id, inst.definition_id)}
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
      </Card>
    </Stack>
  );
}
