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
                    <Text size="sm" ff="monospace" fw={700}>{inst.id}</Text>
                  </td>
                  <td>{getStatusBadge(inst.status)}</td>
                  <td>
                    <Group gap={4}>
                      {(inst.active_nodes || []).map((nodeId: string) => (
                        <Badge key={nodeId} size="xs" variant="outline" color="blue">{nodeId}</Badge>
                      ))}
                      {(!inst.active_nodes || inst.active_nodes.length === 0) && inst.status === 'active' && (
                        <Text size="xs" c="dimmed">No active tokens</Text>
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
