import React from 'react';
import { 
  Table, 
  Card, 
  Text, 
  Button, 
  Group, 
  Stack, 
  ThemeIcon, 
  TextInput, 
  ActionIcon, 
  Avatar, 
  Tooltip, 
  Box,
  Badge
} from '@mantine/core';
import { 
  Search, 
  Filter, 
  CheckCircle, 
  MoreHorizontal, 
  Clock, 
  User, 
  ExternalLink,
  Settings,
  FileCode,
  Hand,
  Briefcase, ListChecks
} from 'lucide-react';
import { useTasks, useCompleteTask } from '../hooks/useProcess';
import { PageHeader } from '../components/PageHeader';
import { StatusBadge } from '../components/StatusBadge';
import { TableLoadingState, ErrorState, EmptyState } from '../components/state';

const getTaskIcon = (type: string) => {
  switch (type) {
    case 'userTask': return User;
    case 'serviceTask': return Settings;
    case 'scriptTask': return FileCode;
    case 'manualTask': return Hand;
    case 'businessRuleTask': return Briefcase;
    default: return Clock;
  }
};

export function TaskList() {
  const { data, isLoading, error, refetch } = useTasks();
  const completeTask = useCompleteTask();


  const tasks = data?.tasks || [];

  return (
    <Stack gap="xl">
      <PageHeader 
        title="My Tasks" 
        description="Manage and complete your assigned tasks."
        actions={
          <Button variant="filled" color="indigo" leftSection={<CheckCircle size={16} />}>
            Batch Complete
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput 
                placeholder="Search tasks by name or ID..." 
                leftSection={<Search size={16} />} 
                style={{ flex: 1, maxWidth: 400 }}
                variant="filled"
                radius="md"
              />
              <Button variant="light" leftSection={<Filter size={16} />} radius="md">Filter</Button>
            </Group>
            <ActionIcon aria-label="More actions" variant="subtle" color="gray">
              <MoreHorizontal size={20} />
            </ActionIcon>
          </Group>
        </Box>

        {/*
          Loading and error render inside the page rather than replacing it.
          The previous early return swapped the whole page — title, filters,
          actions — for one line of text, so the layout jumped when data
          arrived and a failed request looked identical to an empty list.
        */}
        {isLoading ? (
          <TableLoadingState rows={5} columns={4} />
        ) : error ? (
          <ErrorState error={error} action="load your tasks" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th style={{ width: 40 }}>
                  <TextInput type="checkbox" size="xs" />
                </Table.Th>
                <Table.Th>Task Name</Table.Th>
                <Table.Th>Assignee</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Variables</Table.Th>
                <Table.Th>Due Date</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {tasks.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={4}>
                    <EmptyState icon={ListChecks} title="No tasks yet" description="Tasks appear here when a running process reaches a step that needs a person." />
                  </Table.Td>
                </Table.Tr>
              ) : (
                tasks.map((task: any) => (
                  <Table.Tr key={task.id}>
                    <Table.Td>
                      <TextInput type="checkbox" size="xs" />
                    </Table.Td>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon 
                          color={task.status === 'completed' ? 'green' : (task.type === 'userTask' ? 'blue' : 'teal')} 
                          variant="light" 
                          radius="md" 
                          size="md"
                        >
                          {React.createElement(getTaskIcon(task.type), { size: 16 })}
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{task.name}</Text>
                          <Text size="xs" c="dimmed">ID: {task.id.slice(0, 8)}...</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs">
                        <Avatar size="sm" radius="xl" color="indigo">
                          <User size={14} />
                        </Avatar>
                        <Text size="sm" fw={500}>Current User</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <StatusBadge status={task.status} />
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4} wrap="wrap">
                        {task.variables && Object.keys(task.variables).length > 0 && 
                          Object.entries(task.variables).map(([key, value]) => (
                            <Badge key={key} size="xs" variant="outline" color="gray" radius="sm">
                              {key}: {String(value)}
                            </Badge>
                          ))
                        }
                        {(!task.variables || Object.keys(task.variables).length === 0) && (
                          <Text size="xs" c="dimmed italic">Empty</Text>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label="Task should be completed soon">
                        <Group gap={4}>
                          <Text size="sm" c="dimmed">Today</Text>
                          <Badge size="xs" variant="light" color="red">Overdue</Badge>
                        </Group>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Button 
                          size="xs" 
                          variant="light"
                          color="indigo"
                          onClick={() => completeTask.mutate(task.id)}
                          loading={completeTask.isPending}
                          disabled={task.status === 'completed'}
                          leftSection={<CheckCircle size={14} />}
                        >
                          Complete
                        </Button>
                        <ActionIcon aria-label="Open task" variant="subtle" color="gray">
                          <ExternalLink size={16} />
                        </ActionIcon>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
        )}
      </Card>
    </Stack>
  );
}
