import { Alert, Badge, Button, Card, Group, Modal, Select, Stack, Table, Text } from '@mantine/core';
import { AlertTriangle } from 'lucide-react';
import dayjs from 'dayjs';
import { useMemo, useState } from 'react';

import { reasonReady, reasonToSend } from '../domain/taskDelegation';
import { whyNobodyCanAct } from '../domain/strandedTasks';
import { useUsers } from '../hooks/useIdentityDirectory';
import { useStrandedTasks } from '../hooks/useParticipants';
import { useAssignTask } from '../hooks/useTasks';
import type { StrandedTask } from '../services/domains/participantService';
import { useAppStore } from '../store/useAppStore';
import { ReasonField } from './inbox/ReasonField';

/**
 * The open tasks nobody can act on: held by somebody who left or was
 * deactivated, or offered only to people and groups with nobody left who can
 * take them. Without this they wait for ever, because a task names who may
 * do it and nothing tells anybody when that name stops meaning someone.
 *
 * Shows nothing when there are none, and nothing to somebody the server does
 * not let read the report: handing work over is an operator's.
 */
export function StrandedTasks() {
  const { data: stranded = [], error } = useStrandedTasks();
  const [handing, setHanding] = useState<StrandedTask | null>(null);

  if (error || stranded.length === 0) return null;

  return (
    <Card withBorder radius="lg" p="md">
      <Stack gap="sm">
        <Group gap="xs">
          <Text fw={600}>Nobody can act on these</Text>
          <Badge color="red" variant="light">{stranded.length}</Badge>
        </Group>
        <Alert color="red" variant="light" icon={<AlertTriangle size={16} aria-hidden />}>
          These tasks wait for a person or group that can no longer act: somebody deactivated or no longer here, a
          group that was deleted, or a group with nobody active left. They will wait for ever until somebody hands
          them over.
        </Alert>
        <Table.ScrollContainer minWidth={720}>
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Task</Table.Th>
                <Table.Th>Why nobody can act</Table.Th>
                <Table.Th>Waiting since</Table.Th>
                <Table.Th aria-label="Actions" />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {stranded.map((task) => (
                <Table.Tr key={task.task_id}>
                  <Table.Td>
                    <Text size="sm" fw={500}>{task.name}</Text>
                    {task.instance_id && <Text size="xs" c="dimmed" ff="monospace">{task.instance_id.slice(0, 8)}</Text>}
                  </Table.Td>
                  <Table.Td><Text size="sm" c="red.8">{whyNobodyCanAct(task)}</Text></Table.Td>
                  <Table.Td>
                    <Text size="sm" c="dimmed">{task.since ? dayjs(task.since).fromNow() : ''}</Text>
                  </Table.Td>
                  <Table.Td ta="right">
                    <Button size="compact-sm" variant="default" onClick={() => setHanding(task)}>Hand over</Button>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Stack>
      <HandOverDialog task={handing} onClose={() => setHanding(null)} />
    </Card>
  );
}

/**
 * Gives a stranded task to somebody who can act on it. The reason is always
 * asked: whoever hands it over does not hold it, and the trail keeps why.
 */
function HandOverDialog({ task, onClose }: { task: StrandedTask | null; onClose: () => void }) {
  const organizationId = useAppStore((state) => state.currentOrganizationId);
  const { data: usersData } = useUsers(organizationId);
  const assign = useAssignTask();
  const [assignee, setAssignee] = useState<string | null>(null);
  const [reason, setReason] = useState('');
  const users = useMemo(
    () => (usersData?.users ?? []).map((u) => ({ value: u.username, label: u.full_name || u.username })),
    [usersData],
  );

  const close = () => {
    setAssignee(null);
    setReason('');
    onClose();
  };

  return (
    <Modal opened={!!task} onClose={close} title={<Text fw={700}>Hand over {task?.name}</Text>} radius="md">
      {task && (
        <Stack>
          <Text size="sm" c="dimmed">{whyNobodyCanAct(task)}.</Text>
          <Select
            label="Hand it to"
            description="It lands in their inbox. The hand-over is kept on the task's history with your name."
            placeholder="Pick a person"
            data={users}
            value={assignee}
            onChange={setAssignee}
            searchable
          />
          <ReasonField value={reason} onChange={setReason} />
          <Group justify="flex-end" mt="md">
            <Button variant="default" onClick={close}>Cancel</Button>
            <Button
              disabled={!assignee || !reasonReady('required', reason)}
              loading={assign.isPending}
              onClick={() => {
                if (!assignee) return;
                assign.mutate(
                  { id: task.task_id, userId: assignee, reason: reasonToSend('required', reason) },
                  { onSuccess: close },
                );
              }}
            >
              Hand over
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  );
}
