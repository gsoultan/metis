import { Accordion, Button, Card, Group, Modal, ScrollArea, Stack, Table, Text } from '@mantine/core';
import { ClipboardList, Clock, Info, Search } from 'lucide-react';

import { BusinessTimeline } from '../BusinessTimeline';
import { TaskForm } from '../TaskForm';
import type { Task } from '../../services/types';

interface CompleteTaskModalProps {
  /** The task being completed; the modal is open while it is set. */
  task: Task | null;
  onClose: () => void;
  /** Called with the values of the task's form, or {} for a manual task. */
  onSubmit: (values: Record<string, unknown>) => void;
}

/**
 * The one way a person completes a task from the web app: its instructions,
 * the process context, and the task's own form.
 *
 * Every page that offers Complete opens this. A button that completed a task
 * directly sent no variables, so an approval was finished without the person
 * ever saying yes or no.
 */
export function CompleteTaskModal({ task, onClose, onSubmit }: CompleteTaskModalProps) {
  return (
  <Modal
    opened={!!task}
    onClose={onClose}
    title={
      <Group gap="xs">
        <ClipboardList size={18} color="var(--mantine-color-blue-6)" />
        <Text fw={700}>Complete Task: {task?.name}</Text>
      </Group>
    }
    size="lg"
    radius="md"
  >
    {task && (
      <Stack py="md">
        {task.description && (
          <Card withBorder padding="sm" radius="md" bg="var(--mantine-color-blue-0)">
            <Group gap="xs" mb={4}>
              <Info size={16} color="var(--mantine-color-blue-6)" />
              <Text fw={600} size="sm" c="blue.8">Instructions</Text>
            </Group>
            <Text size="sm">{task.description}</Text>
          </Card>
        )}

        <Accordion variant="separated" radius="md">
          <Accordion.Item value="context">
            <Accordion.Control icon={<Search size={16} />}>
              Process Context (Read-only Variables)
            </Accordion.Control>
            <Accordion.Panel>
              <TaskContextTable variables={task.variables} />
            </Accordion.Panel>
          </Accordion.Item>
          <Accordion.Item value="timeline">
            <Accordion.Control icon={<Clock size={16} />}>
              Activity Timeline
            </Accordion.Control>
            <Accordion.Panel>
              <BusinessTimeline instanceId={task.instance?.id ?? ''} />
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>

        {task.type === 'manualTask' ? (
          <Stack py="md">
            <Text>Please confirm that you have completed the physical action required for this task: <b>{task.name}</b>.</Text>
            <Group justify="flex-end" mt="xl">
              <Button variant="default" onClick={onClose}>Cancel</Button>
              <Button color="blue" onClick={() => onSubmit({})}>
                Confirm Completion
              </Button>
            </Group>
          </Stack>
        ) : (
          <TaskForm 
            fields={(() => {
              try {
                return JSON.parse(task.formDefinition || '[]');
              } catch (error) {
                console.warn('Task form definition is not valid JSON; rendering an empty form', error);
                return [];
              }
            })()} 
            variables={task.variables || {}}
            onSubmit={onSubmit}
            loading={false}
          />
        )}
      </Stack>
    )}
  </Modal>
  );
}

function TaskContextTable({ variables }: { variables: Record<string, unknown> | undefined }) {
  if (!variables) return null;
  const entries = Object.entries(variables);
  if (entries.length === 0) {
    return <Text size="sm" c="dimmed" py="md">No process variables available.</Text>;
  }

  return (
    <ScrollArea h={300} py="sm">
      <Table striped withTableBorder withColumnBorders>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Variable Name</Table.Th>
            <Table.Th>Value</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {entries.map(([key, val]) => (
            <Table.Tr key={key}>
              <Table.Td><Text fw={500} size="sm">{key}</Text></Table.Td>
              <Table.Td>
                <Text size="sm" style={{ wordBreak: 'break-all' }}>
                  {typeof val === 'object' ? JSON.stringify(val) : String(val)}
                </Text>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </ScrollArea>
  );
}
