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
  Badge,
  Checkbox
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
import { shortCode, taskReference } from '../domain/taskReference';
import { taskFacts } from '../domain/taskFacts';
import { urgencyOf } from '../domain/taskUrgency';
import dayjs from 'dayjs';
import { useTranslation } from '../i18n/context';
import type { Task } from '../services/types';

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

/**
 * One task, for a screen too narrow to hold the table.
 *
 * At 375px the table rendered its first two columns — Task Name and Assignee —
 * and dropped Status, Variables, Due Date and Actions, with no horizontal
 * scroll affordance to say they existed. So the Complete button was unreachable
 * and an approver on a phone could read their queue and do nothing about it.
 * That is the most likely mobile user of a BPM tool.
 *
 * A card carries the same facts in reading order and keeps the action.
 */
function TaskCard({
  task,
  onComplete,
  completing,
}: {
  task: Task;
  onComplete: (id: string) => void;
  completing: boolean;
}) {
  const reference = taskReference(task.variables as Record<string, unknown> | undefined, task.instance?.id);
  const { facts } = taskFacts(task.variables as Record<string, unknown> | undefined, 2);
  const urgency = urgencyOf(task);

  return (
    <Card withBorder radius="md" p="md">
      <Stack gap="sm">
        <Group justify="space-between" wrap="nowrap" align="flex-start">
          <Stack gap={2} style={{ minWidth: 0 }}>
            <Text fw={700} size="sm">{task.name}</Text>
            <Text size="xs" c="dimmed" lineClamp={2}>
              {reference.label || `Reference ${reference.code}`}
            </Text>
          </Stack>
          <StatusBadge status={task.status} />
        </Group>

        {facts.length > 0 && (
          <Stack gap={2}>
            {facts.map((fact) => (
              <Text key={fact.label} size="xs">
                <Text span c="dimmed">{fact.label}: </Text>
                {fact.value}
              </Text>
            ))}
          </Stack>
        )}

        <Group justify="space-between" wrap="nowrap">
          {task.dueDate ? (
            <Group gap={6}>
              <Text size="xs" c="dimmed">{dayjs(task.dueDate).format('D MMM')}</Text>
              {urgency.label && (
                <Badge size="sm" variant="light" color={urgency.color}>{urgency.label}</Badge>
              )}
            </Group>
          ) : (
            <Text size="xs" c="dimmed">No due date</Text>
          )}
          <Button
            size="xs"
            variant="light"
            color="indigo"
            onClick={() => onComplete(task.id)}
            loading={completing}
            disabled={task.status === 'completed'}
            leftSection={<CheckCircle size={14} />}
          >
            Complete
          </Button>
        </Group>
      </Stack>
    </Card>
  );
}

export function TaskList() {
  const { t } = useTranslation();
  const { data, isLoading, error, refetch } = useTasks();
  const completeTask = useCompleteTask();


  const tasks = data?.tasks || [];

  return (
    <Stack gap="xl">
      {/*
        Titled "My Tasks" while the nav entry that reaches it says "All Tasks",
        and described as "your assigned tasks" while listing tasks assigned to
        nobody. Three claims, one screen. The nav label is the honest one: this
        is every task in the project.

        The "Batch Complete" action is gone. It carried no onClick — it was a
        dead control the last honesty pass missed — and it was the filled
        primary button on a page of financial approvals, which is the wrong
        thing to make easiest. Per-item review is the point of an approval step.
        Bulk actions belong in the inbox, where a selection exists to act on.
      */}
      <PageHeader
        title={t('page.allTasks.title')}
        description={t('page.allTasks.subtitle')}
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput
                aria-label="Search tasks"
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
        <>
        {/* Below sm the table cannot show its action column; cards can. */}
        <Stack gap="sm" p="md" hiddenFrom="sm">
          {tasks.length === 0 ? (
            <EmptyState icon={ListChecks} title="No tasks yet" description="Tasks appear here when a running process reaches a step that needs a person." />
          ) : (
            tasks.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                onComplete={(id) => completeTask.mutate({ id })}
                completing={completeTask.isPending}
              />
            ))
          )}
        </Stack>

        <Box visibleFrom="sm">
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th style={{ width: 40 }}>
                  {/*
                    Was <TextInput type="checkbox">, which renders a text input
                    wearing a checkbox type: no label, no checkbox semantics.
                    One of these per row plus this one is the thirteen unlabeled
                    controls axe reports on this page.
                  */}
                  <Checkbox size="xs" aria-label="Select all tasks" />
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
                tasks.map((task) => (
                  <Table.Tr key={task.id}>
                    <Table.Td>
                      <Checkbox size="xs" aria-label={`Select task ${task.name}`} />
                    </Table.Td>
                    <Table.Td>
                      <Group gap="sm">
                        {/* The icon says what kind of work this is; the badge
                            beside it says how it is going. Colouring the icon by
                            status as well said the same thing twice, in a
                            different palette. */}
                        <ThemeIcon
                          color={task.type === 'userTask' ? 'blue' : 'teal'}
                          variant="light"
                          radius="md"
                          size="md"
                        >
                          {React.createElement(getTaskIcon(task.type), { size: 16 })}
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{task.name}</Text>
                          {/* What the work is about, not a fragment of its
                              identifier. See domain/taskReference.ts. */}
                          <Text size="xs" c="dimmed" lineClamp={1}>
                            {taskReference(task.variables as Record<string, unknown> | undefined, task.instance?.id).label
                              || `Reference ${shortCode(task.instance?.id)}`}
                          </Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      {/*
                        This cell printed the literal string "Current User" for
                        every row, including rows whose status is AVAILABLE —
                        i.e. rows that are assigned to nobody. It read as data,
                        it was wrong, and it contradicted the inbox, which was
                        simultaneously reporting that nothing was assigned to
                        you. Absent data renders as absent.
                      */}
                      {task.assignee?.username ? (
                        <Group gap="xs">
                          <Avatar size="sm" radius="xl" color="indigo">
                            <User size={14} />
                          </Avatar>
                          <Text size="sm" fw={500}>{task.assignee.username}</Text>
                        </Group>
                      ) : (
                        <Text size="sm" c="dimmed" fs="italic">Unassigned</Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <StatusBadge status={task.status} />
                    </Table.Td>
                    <Table.Td>
                      {/*
                        Was one outlined badge per process variable, rendered as
                        `KEY: value` and clipped by the column — six per row, so
                        none of them readable and the row 137px tall. See
                        domain/taskFacts.ts for what is chosen and why.
                      */}
                      {(() => {
                        const { facts, hidden } = taskFacts(
                          task.variables as Record<string, unknown> | undefined,
                        );
                        if (facts.length === 0) {
                          return <Text size="xs" c="dimmed" fs="italic">No details</Text>;
                        }
                        /*
                          One line of values, not a stack of "Label: value".
                          Badges came back as "APPROVAL LEVEL: …" because
                          Mantine uppercases a Badge label, and stacking three
                          of them is what made each row 137px tall. Putting the
                          labels back as text then clipped the values instead —
                          the column is not wide enough for both.

                          So: the values, which are what distinguishes one row
                          from another, and the labels in the tooltip for
                          whoever needs to know which field is which.
                        */
                        const detail = facts.map((f) => `${f.label}: ${f.value}`).join('\n');
                        return (
                          <Tooltip label={detail} multiline w={260} withArrow>
                            <Stack gap={0}>
                              <Text size="xs" lineClamp={2}>
                                {facts.map((f) => f.value).join(' · ')}
                              </Text>
                              {hidden > 0 && (
                                <Text size="xs" c="dimmed">+{hidden} more</Text>
                              )}
                            </Stack>
                          </Tooltip>
                        );
                      })()}
                    </Table.Td>
                    <Table.Td>
                      {/*
                        This cell was hardcoded: every row rendered "Today" and
                        a red "Overdue" badge whatever the task's actual due
                        date, and the column was narrow enough to clip the badge
                        to "OVE…". So the one thing a queue has to communicate —
                        which of these is late — was fabricated and unreadable.

                        `urgencyOf` is the same computation the inbox already
                        rates its rows with, so the two screens now agree.
                      */}
                      {(() => {
                        const urgency = urgencyOf(task);
                        if (!task.dueDate) {
                          return <Text size="sm" c="dimmed">No due date</Text>;
                        }
                        return (
                          <Stack gap={2}>
                            <Text size="sm" c="dimmed">
                              {dayjs(task.dueDate).format('D MMM')}
                            </Text>
                            {urgency.label && (
                              /*
                                `light`, not `filled`. A filled badge is white
                                text on the accent, which axe measured at 3.58:1
                                on the orange — below the 4.5:1 AA needs. The
                                light variant is dark text on a tint and passes.
                              */
                              <Badge size="sm" variant="light" color={urgency.color}>
                                {urgency.label}
                              </Badge>
                            )}
                          </Stack>
                        );
                      })()}
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Button 
                          size="xs" 
                          variant="light"
                          color="indigo"
                          onClick={() => completeTask.mutate({ id: task.id })}
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
        </Box>
        </>
        )}
      </Card>
    </Stack>
  );
}
