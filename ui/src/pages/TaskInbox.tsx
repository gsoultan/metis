import { 
  Card, 
  Text, 
  Button, 
  Group, 
  Stack, 
  ThemeIcon, 
  Badge,
  Tabs,
  TextInput,
  ActionIcon,
  Tooltip,
  Modal,
  Select,
  Menu,
  NumberInput,
  Table,
  ScrollArea,
  Accordion,
  Checkbox,
  Transition,
  Avatar,
  SegmentedControl,
  Grid,
  Paper,
  Center,
  Box,
} from '@mantine/core';
import { DateInput } from '@mantine/dates';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
dayjs.extend(relativeTime);
import { 
  CheckCircle, 
  ClipboardList,
  Clock,
  Search,
  Filter,
  User,
  Users,
  MoreVertical,
  ExternalLink,
  Edit2,
  Info,
  LayoutGrid,
  List,
  AlertTriangle,
  UserPlus,
  ArrowDownLeft,
} from 'lucide-react';
import { PageHeader } from '../components/PageHeader';
import { TaskForm } from '../components/TaskForm';
import { BusinessTimeline } from '../components/BusinessTimeline';
import { useTaskInbox } from '../hooks/useTaskInbox';
import { useNavigate } from '@tanstack/react-router';

function TaskContextTable({ variables }: { variables: Record<string, any> | undefined }) {
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

interface TaskRowProps {
  task: any;
  isSelected: boolean;
  onToggleSelection: (id: string) => void;
  onClaim: (id: string) => void;
  onUnclaim: (id: string) => void;
  onComplete: (task: any) => void;
  onEdit: (task: any) => void;
  onReassign: (task: any) => void;
  navigate: (args: any) => void;
}

function TaskRow({ task, isSelected, onToggleSelection, onClaim, onUnclaim, onComplete, onEdit, onReassign, navigate }: TaskRowProps) {
  return (
    <Table.Tr bg={isSelected ? 'blue.0' : undefined}>
      <Table.Td>
        <Checkbox 
          checked={isSelected} 
          onChange={() => onToggleSelection(task.id)} 
          radius="sm"
        />
      </Table.Td>
      <Table.Td>
        <Group gap="sm">
          <ThemeIcon 
            color={task.priority > 50 ? "red" : "blue"} 
            variant="light" 
            radius="md" 
            size="lg"
          >
            <CheckCircle size={20} />
          </ThemeIcon>
          <Stack gap={0}>
            <Text fw={700} size="sm">{task.name}</Text>
            <Group gap={4}>
              <Tooltip label="View Process Instance Path">
                <ActionIcon 
                  variant="subtle" 
                  size="xs" 
                  color="blue"
                  onClick={(e) => {
                    e.stopPropagation();
                    navigate({
                      to: '/designer',
                      search: { instanceId: task.instance_id, definitionId: task.definition_id }
                    });
                  }}
                >
                  <ExternalLink size={12} />
                </ActionIcon>
              </Tooltip>
              {task.priority > 0 && (
                <Badge size="xs" color={task.priority > 50 ? "red" : "orange"} variant="light">
                  Priority: {task.priority}
                </Badge>
              )}
              {task.form_key && (
                <Badge size="xs" variant="outline" color="gray">Form: {task.form_key}</Badge>
              )}
            </Group>
          </Stack>
        </Group>
      </Table.Td>
      <Table.Td>
        <Group gap="xs">
          {task.assignee ? (
            <Tooltip label={`Assigned to ${task.assignee}`}>
              <Group gap="xs">
                <ThemeIcon size="sm" variant="subtle" color="blue">
                  <User size={14} />
                </ThemeIcon>
                <Avatar size="sm" radius="xl" color="blue" variant="light">
                  {task.assignee.substring(0, 2).toUpperCase()}
                </Avatar>
                <Text size="sm">{task.assignee}</Text>
              </Group>
            </Tooltip>
          ) : (
            <Tooltip label="Available for candidates">
              <Group gap="xs">
                <ThemeIcon size="sm" variant="subtle" color="orange">
                  <Users size={14} />
                </ThemeIcon>
                <Text size="sm" c="dimmed">Unclaimed</Text>
              </Group>
            </Tooltip>
          )}
        </Group>
      </Table.Td>
      <Table.Td>
        <Stack gap={0}>
          <Group gap={4}>
            <Text size="sm" fw={600}>{task.instance_id.substring(0, 8)}</Text>
            <Tooltip label="View Process Instance Path">
              <ActionIcon 
                variant="subtle" 
                size="xs"
                onClick={() => navigate({
                  to: '/designer',
                  search: { instanceId: task.instance_id, definitionId: task.definition_id }
                })}
              >
                <ExternalLink size={12} />
              </ActionIcon>
            </Tooltip>
          </Group>
          <Text size="xs" c="dimmed">Created: {new Date(task.created_at).toLocaleDateString()}</Text>
        </Stack>
      </Table.Td>
      <Table.Td>
        <Group gap="xs">
          <Clock size={14} color={task.due_date && new Date(task.due_date) < new Date() ? "red" : "gray"} />
          <Stack gap={0}>
            {task.due_date ? (
              <Text size="sm" c={new Date(task.due_date) < new Date() ? "red" : undefined}>
                {new Date(task.due_date).toLocaleString()}
              </Text>
            ) : (
              <Text size="sm" c="dimmed">No due date</Text>
            )}
          </Stack>
        </Group>
      </Table.Td>
      <Table.Td>
        <Badge 
          color={
            task.status === 'unclaimed' ? 'orange' : 
            task.status === 'claimed' ? 'blue' : 
            task.status === 'escalated' ? 'red' : 'green'
          } 
          variant="light"
          radius="sm"
        >
          {task.status}
        </Badge>
      </Table.Td>
      <Table.Td ta="right">
        <Group gap="xs" justify="flex-end">
          {task.status === 'unclaimed' ? (
            <Button 
              size="xs" 
              variant="filled" 
              color="blue"
              onClick={() => onClaim(task.id)}
            >
              Claim Task
            </Button>
          ) : (
            <>
              <Tooltip label="Release back to group">
                <ActionIcon 
                  variant="light" 
                  color="gray"
                  onClick={() => onUnclaim(task.id)}
                >
                  <Users size={16} />
                </ActionIcon>
              </Tooltip>
              <Button 
                size="xs" 
                variant="filled" 
                color={task.type === 'manualTask' ? 'blue' : 'green'}
                onClick={() => onComplete(task)}
                leftSection={task.type === 'manualTask' ? <CheckCircle size={14} /> : undefined}
              >
                {task.type === 'manualTask' ? 'Mark as Done' : 'Complete'}
              </Button>
            </>
          )}
          <Menu shadow="md" width={200} position="bottom-end">
            <Menu.Target>
              <ActionIcon variant="subtle" color="gray">
                <MoreVertical size={16} />
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>Task Management</Menu.Label>
              <Menu.Item 
                leftSection={<Edit2 size={14} />} 
                onClick={() => onEdit(task)}
              >
                Edit Task Details
              </Menu.Item>
              <Menu.Item 
                leftSection={<User size={14} />}
                onClick={() => onReassign(task)}
              >
                Reassign Task
              </Menu.Item>
              <Menu.Divider />
              <Menu.Item 
                leftSection={<ExternalLink size={14} />}
                onClick={() => navigate({
                  to: '/designer',
                  search: { instanceId: task.instance_id, definitionId: task.definition_id }
                })}
              >
                View Process Path
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Table.Td>
    </Table.Tr>
  );
}

function TaskCard({ task, isSelected, onToggleSelection, onClaim, onComplete, onEdit, onReassign, navigate }: any) {
  const isOverdue = task.due_date && new Date(task.due_date) < new Date();
  
  return (
    <Card withBorder padding="md" radius="md" shadow="sm" style={{ 
      borderLeft: `4px solid ${task.priority > 50 ? 'var(--mantine-color-red-6)' : 'var(--mantine-color-blue-6)'}`,
      backgroundColor: isSelected ? 'var(--mantine-color-blue-0)' : undefined
    }}>
      <Group justify="space-between" mb="xs">
        <Group gap="xs">
          <Checkbox 
            checked={isSelected} 
            onChange={() => onToggleSelection(task.id)} 
            size="xs"
          />
          <Badge size="xs" color={
              task.status === 'unclaimed' ? 'orange' : 
              task.status === 'claimed' ? 'blue' : 
              task.status === 'escalated' ? 'red' : 'green'
            }>
            {task.status}
          </Badge>
        </Group>
        <Menu shadow="md" width={200} position="bottom-end">
            <Menu.Target>
              <ActionIcon variant="subtle" color="gray" size="sm">
                <MoreVertical size={14} />
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item leftSection={<Edit2 size={12} />} onClick={() => onEdit(task)}>Edit</Menu.Item>
              <Menu.Item leftSection={<User size={12} />} onClick={() => onReassign(task)}>Reassign</Menu.Item>
              <Menu.Divider />
              <Menu.Item 
                leftSection={<ExternalLink size={12} />}
                onClick={() => navigate({
                  to: '/designer',
                  search: { instanceId: task.instance_id, definitionId: task.definition_id }
                })}
              >
                View Process Path
              </Menu.Item>
            </Menu.Dropdown>
        </Menu>
      </Group>

      <Text fw={700} size="sm" mb={4} lineClamp={1}>{task.name}</Text>
      
      <Group gap={4} mb="md">
        {task.priority > 0 && (
          <Badge size="xs" color={task.priority > 50 ? "red" : "orange"} variant="light">
            Priority: {task.priority}
          </Badge>
        )}
        {task.form_key && (
          <Badge size="xs" variant="outline" color="gray">Form: {task.form_key}</Badge>
        )}
      </Group>

      <Stack gap={8}>
        <Group gap="xs">
          <Avatar size="xs" radius="xl" color="blue" variant="light">
            {task.assignee ? task.assignee.substring(0, 2).toUpperCase() : '?'}
          </Avatar>
          <Text size="xs" c="dimmed" lineClamp={1}>{task.assignee || 'Unassigned'}</Text>
        </Group>

        <Group gap="xs">
          <Clock size={12} color={isOverdue ? "var(--mantine-color-red-6)" : "var(--mantine-color-gray-6)"} />
          <Group gap={4}>
            <Text size="xs" c={isOverdue ? "red.6" : "dimmed"}>
              {task.due_date ? dayjs(task.due_date).fromNow() : 'No due date'}
            </Text>
            {isOverdue && (
              <Tooltip label="Task is overdue!">
                <AlertTriangle size={12} color="var(--mantine-color-red-6)" />
              </Tooltip>
            )}
          </Group>
        </Group>
      </Stack>

      <Group grow mt="md">
        {task.status === 'unclaimed' ? (
          <Button size="compact-xs" variant="light" color="blue" onClick={() => onClaim(task.id)}>
            Claim
          </Button>
        ) : (
          <Button size="compact-xs" color={task.type === 'manualTask' ? 'blue' : 'green'} onClick={() => onComplete(task)}>
            {task.type === 'manualTask' ? 'Done' : 'Complete'}
          </Button>
        )}
      </Group>
    </Card>
  );
}

function KanbanView({ tasks, selectedTaskIds, onToggleSelection, onClaim, onUnclaim, onComplete, onEdit, onReassign, searchQuery, navigate }: any) {
  const columns = [
    { id: 'unclaimed', title: 'Unclaimed', status: 'unclaimed', color: 'orange' },
    { id: 'claimed', title: 'In Progress', status: 'claimed', color: 'blue' },
    { id: 'completed', title: 'Completed', status: 'completed', color: 'green' },
  ];

  const filteredTasks = tasks.filter((t: any) => {
    if (!searchQuery) return true;
    const q = searchQuery.toLowerCase();
    return t.name?.toLowerCase().includes(q) || t.id?.toLowerCase().includes(q);
  });

  return (
    <Grid gap="md">
      {columns.map(col => (
        <Grid.Col span={{ base: 12, md: 4 }} key={col.id}>
          <Paper p="md" radius="lg" bg="gray.0" withBorder h="100%" style={{ minHeight: 500 }}>
            <Group justify="space-between" mb="md">
              <Group gap="xs">
                <Badge color={col.color} variant="filled" size="sm">{col.title}</Badge>
                <Text size="xs" c="dimmed" fw={500}>
                  {filteredTasks.filter((t: any) => t.status === col.status).length} Tasks
                </Text>
              </Group>
            </Group>
            <Stack gap="md">
              {filteredTasks.filter((t: any) => t.status === col.status).map((task: any) => (
                <TaskCard 
                  key={task.id} 
                  task={task} 
                  isSelected={selectedTaskIds.includes(task.id)}
                  onToggleSelection={onToggleSelection}
                  onClaim={onClaim} 
                  onUnclaim={onUnclaim} 
                  onComplete={onComplete} 
                  onEdit={onEdit} 
                  onReassign={onReassign} 
                  navigate={navigate}
                />
              ))}
              {filteredTasks.filter((t: any) => t.status === col.status).length === 0 && (
                <Stack align="center" py={40} gap="xs">
                  <ClipboardList size={24} color="var(--mantine-color-gray-4)" />
                  <Text size="xs" c="dimmed">No tasks in this stage</Text>
                </Stack>
              )}
            </Stack>
          </Paper>
        </Grid.Col>
      ))}
    </Grid>
  );
}

export function TaskInbox() {
  const navigate = useNavigate();
  const {
    currentUser,
    setCurrentUser,
    activeTab,
    setActiveTab,
    searchQuery,
    setSearchQuery,
    selectedTask,
    setSelectedTask,
    editingTask,
    setEditingTask,
    handleSort,
    availableUsers,
    reassignModalOpened,
    setReassignModalOpened,
    taskToReassign,
    setTaskToReassign,
    newAssignee,
    setNewAssignee,
    assignedLoading,
    candidateLoading,
    assignedCount,
    candidateCount,
    currentTasks,
    viewMode,
    setViewMode,
    selectedTaskIds,
    setSelectedTaskIds,
    toggleSelection,
    handleBulkClaim,
    handleBulkUnclaim,
    allTasks,
    allTasksLoading,
    handleClaim,
    handleUnclaim,
    handleComplete,
    handleAssign,
    updateTaskMutation,
  } = useTaskInbox();

  const onFormSubmit = (values: any) => {
    if (selectedTask) {
      handleComplete(selectedTask.id, values);
    }
  };

  const onCompleteClick = (task: any) => {
    if (task.type === 'manualTask') {
      handleComplete(task.id, {});
    } else {
      setSelectedTask(task);
    }
  };

  const onReassignClick = (task: any) => {
    setTaskToReassign(task);
    setNewAssignee(task.assignee || null);
    setReassignModalOpened(true);
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Task Inbox" 
        description={`Manage and complete tasks for ${currentUser}.`}
        actions={
          <Group gap="sm">
            <SegmentedControl
              size="xs"
              value={viewMode}
              onChange={(val: any) => setViewMode(val)}
              data={[
                { label: <Center><List size={14} /><Box ml={4}>Table</Box></Center>, value: 'table' },
                { label: <Center><LayoutGrid size={14} /><Box ml={4}>Kanban</Box></Center>, value: 'kanban' },
              ]}
              radius="md"
            />
            <Select
              size="xs" 
              data={availableUsers} 
              value={currentUser} 
              onChange={(val: string | null) => val && setCurrentUser(val)} 
              leftSection={<User size={14} />}
              placeholder="Switch User"
              w={150}
              searchable
            />
            <TextInput 
              placeholder="Search tasks..." 
              leftSection={<Search size={16} />}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.currentTarget.value)}
              radius="md"
              size="sm"
              w={250}
            />
            <Button variant="light" leftSection={<Filter size={16} />}>Filter</Button>
          </Group>
        }
      />

      <Transition mounted={selectedTaskIds.length > 0} transition="slide-up" duration={200} timingFunction="ease">
        {(styles) => (
          <Paper 
            shadow="lg" 
            p="md" 
            withBorder 
            radius="lg" 
            bg="blue.6" 
            style={{ 
              ...styles, 
              position: 'fixed', 
              bottom: 40, 
              left: '50%', 
              transform: 'translateX(-50%)', 
              zIndex: 100,
              width: 'fit-content',
              minWidth: 400
            }}
          >
            <Group justify="space-between" wrap="nowrap">
              <Group gap="md">
                <Badge color="white" variant="white" circle size="lg">{selectedTaskIds.length}</Badge>
                <Text fw={700} c="white">Tasks Selected</Text>
              </Group>
              <Group gap="sm">
                {activeTab === 'available' ? (
                  <Button 
                    variant="white" 
                    color="blue" 
                    size="xs" 
                    leftSection={<UserPlus size={14} />}
                    onClick={handleBulkClaim}
                  >
                    Bulk Claim
                  </Button>
                ) : (
                  <Button 
                    variant="white" 
                    color="gray" 
                    size="xs" 
                    leftSection={<ArrowDownLeft size={14} />}
                    onClick={handleBulkUnclaim}
                  >
                    Bulk Release
                  </Button>
                )}
                <Button 
                  variant="transparent" 
                  c="white" 
                  size="xs" 
                  onClick={() => setSelectedTaskIds([])}
                >
                  Cancel
                </Button>
              </Group>
            </Group>
          </Paper>
        )}
      </Transition>

      <Tabs value={activeTab} onChange={setActiveTab} variant="pills" radius="md">
        {viewMode === 'table' && (
          <Tabs.List mb="md">
            <Tabs.Tab 
              value="assigned" 
              leftSection={<User size={16} />}
              rightSection={
                assignedCount > 0 && (
                  <Badge size="xs" variant="filled" circle color="blue">
                    {assignedCount}
                  </Badge>
                )
              }
            >
              Assigned to Me
            </Tabs.Tab>
            <Tabs.Tab 
              value="available" 
              leftSection={<Users size={16} />}
              rightSection={
                candidateCount > 0 && (
                  <Badge size="xs" variant="filled" circle color="orange">
                    {candidateCount}
                  </Badge>
                )
              }
            >
              Available to Claim
            </Tabs.Tab>
          </Tabs.List>
        )}

        {viewMode === 'table' ? (
          <Card shadow="sm" radius="lg" withBorder p={0}>
            <Table.ScrollContainer minWidth={800}>
              <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
                <Table.Thead bg="gray.0">
                  <Table.Tr>
                    <Table.Th w={40}>
                      <Checkbox 
                        checked={selectedTaskIds.length === currentTasks.length && currentTasks.length > 0} 
                        indeterminate={selectedTaskIds.length > 0 && selectedTaskIds.length < currentTasks.length}
                        onChange={() => {
                          if (selectedTaskIds.length === currentTasks.length) {
                            setSelectedTaskIds([]);
                          } else {
                            setSelectedTaskIds(currentTasks.map((t: any) => t.id));
                          }
                        }}
                      />
                    </Table.Th>
                    <Table.Th style={{ cursor: 'pointer' }} onClick={() => handleSort('name')}>Task Info</Table.Th>
                    <Table.Th>Assignment</Table.Th>
                    <Table.Th style={{ cursor: 'pointer' }} onClick={() => handleSort('instance_id')}>Process Instance</Table.Th>
                    <Table.Th style={{ cursor: 'pointer' }} onClick={() => handleSort('due_date')}>Timeline</Table.Th>
                    <Table.Th style={{ cursor: 'pointer' }} onClick={() => handleSort('status')}>Status</Table.Th>
                    <Table.Th ta="right">Actions</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {assignedLoading || candidateLoading ? (
                    <Table.Tr>
                      <Table.Td colSpan={6}>
                        <Group justify="center" py="xl">
                          <Text c="dimmed">Loading tasks...</Text>
                        </Group>
                      </Table.Td>
                    </Table.Tr>
                  ) : currentTasks.length === 0 ? (
                    <Table.Tr>
                      <Table.Td colSpan={6}>
                        <Stack align="center" py={60} gap="sm">
                          <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                            <ClipboardList size={32} />
                          </ThemeIcon>
                          <Text fw={700} size="lg">No tasks found</Text>
                          <Text ta="center" c="dimmed" maw={400}>
                            {searchQuery ? "No tasks match your search criteria." : "Everything is done! You have no pending tasks in this view."}
                          </Text>
                        </Stack>
                      </Table.Td>
                    </Table.Tr>
                  ) : (
                    currentTasks.map((task: any) => (
                      <TaskRow
                        key={task.id}
                        task={task}
                        isSelected={selectedTaskIds.includes(task.id)}
                        onToggleSelection={toggleSelection}
                        onClaim={handleClaim}
                        onUnclaim={handleUnclaim}
                        onComplete={onCompleteClick}
                        onEdit={setEditingTask}
                        onReassign={onReassignClick}
                        navigate={navigate}
                      />
                    ))
                  )}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </Card>
        ) : (
          allTasksLoading ? (
            <Group justify="center" py={100}><Text c="dimmed">Loading Kanban Board...</Text></Group>
          ) : (
            <KanbanView 
              tasks={allTasks}
              selectedTaskIds={selectedTaskIds}
              onToggleSelection={toggleSelection}
              onClaim={handleClaim}
              onUnclaim={handleUnclaim}
              onComplete={onCompleteClick}
              onEdit={setEditingTask}
              onReassign={onReassignClick}
              searchQuery={searchQuery}
              navigate={navigate}
            />
          )
        )}
      </Tabs>

      <Modal
        opened={reassignModalOpened}
        onClose={() => setReassignModalOpened(false)}
        title={
          <Group gap="xs">
            <User size={18} color="var(--mantine-color-blue-6)" />
            <Text fw={700}>Reassign Task: {taskToReassign?.name}</Text>
          </Group>
        }
        radius="md"
      >
        <Stack py="md">
          <Select
            label="New Assignee"
            placeholder="Select user"
            description="Select a user to take responsibility for this task"
            data={availableUsers}
            value={newAssignee}
            onChange={setNewAssignee}
            searchable
            clearable
          />
          <Group justify="flex-end" mt="xl">
            <Button variant="default" onClick={() => setReassignModalOpened(false)}>Cancel</Button>
            <Button 
              color="blue" 
              onClick={() => {
                if (taskToReassign && newAssignee) {
                  handleAssign(taskToReassign.id, newAssignee);
                }
              }} 
              disabled={!newAssignee}
            >
              Confirm Reassignment
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={!!selectedTask}
        onClose={() => setSelectedTask(null)}
        title={
          <Group gap="xs">
            <ClipboardList size={18} color="var(--mantine-color-blue-6)" />
            <Text fw={700}>Complete Task: {selectedTask?.name}</Text>
          </Group>
        }
        size="lg"
        radius="md"
      >
        {selectedTask && (
          <Stack py="md">
            {selectedTask.description && (
              <Card withBorder padding="sm" radius="md" bg="var(--mantine-color-blue-0)">
                <Group gap="xs" mb={4}>
                  <Info size={16} color="var(--mantine-color-blue-6)" />
                  <Text fw={600} size="sm" c="blue.8">Instructions</Text>
                </Group>
                <Text size="sm">{selectedTask.description}</Text>
              </Card>
            )}

            <Accordion variant="separated" radius="md">
              <Accordion.Item value="context">
                <Accordion.Control icon={<Search size={16} />}>
                  Process Context (Read-only Variables)
                </Accordion.Control>
                <Accordion.Panel>
                  <TaskContextTable variables={selectedTask.variables} />
                </Accordion.Panel>
              </Accordion.Item>
              <Accordion.Item value="timeline">
                <Accordion.Control icon={<Clock size={16} />}>
                  Activity Timeline
                </Accordion.Control>
                <Accordion.Panel>
                  <BusinessTimeline instanceId={selectedTask.instance_id} />
                </Accordion.Panel>
              </Accordion.Item>
            </Accordion>

            {selectedTask.type === 'manualTask' ? (
              <Stack py="md">
                <Text>Please confirm that you have completed the physical action required for this task: <b>{selectedTask.name}</b>.</Text>
                <Group justify="flex-end" mt="xl">
                  <Button variant="default" onClick={() => setSelectedTask(null)}>Cancel</Button>
                  <Button color="blue" onClick={() => onFormSubmit({})}>
                    Confirm Completion
                  </Button>
                </Group>
              </Stack>
            ) : (
              <TaskForm 
                fields={(() => {
                  try {
                    return JSON.parse(selectedTask.form_definition || '[]');
                  } catch (error) {
                    console.warn('Task form definition is not valid JSON; rendering an empty form', error);
                    return [];
                  }
                })()} 
                variables={selectedTask.variables || {}}
                onSubmit={onFormSubmit}
                loading={false}
              />
            )}
          </Stack>
        )}
      </Modal>

      <Modal
        opened={!!editingTask}
        onClose={() => setEditingTask(null)}
        title={
          <Group gap="xs">
            <Edit2 size={18} color="var(--mantine-color-blue-6)" />
            <Text fw={700}>Edit Task: {editingTask?.name}</Text>
          </Group>
        }
        size="md"
        radius="md"
      >
        {editingTask && (
          <Stack py="md">
            <TextInput 
              label="Task Name" 
              value={editingTask.name} 
              onChange={(e) => setEditingTask({...editingTask, name: e.currentTarget.value})}
            />
            <NumberInput 
              label="Priority" 
              value={editingTask.priority} 
              onChange={(val) => setEditingTask({...editingTask, priority: val})}
            />
            <DateInput 
              label="Due Date" 
              value={editingTask.due_date ? new Date(editingTask.due_date) : null} 
              onChange={(date: any) => {
                if (!date) {
                  setEditingTask({...editingTask, due_date: null});
                  return;
                }
                const d = new Date(date);
                setEditingTask({...editingTask, due_date: d.toISOString()});
              }}
              clearable
            />
            <Group justify="flex-end" mt="xl">
              <Button variant="default" onClick={() => setEditingTask(null)}>Cancel</Button>
              <Button 
                color="blue" 
                onClick={() => {
                  updateTaskMutation.mutate({
                    id: editingTask.id,
                    name: editingTask.name,
                    priority: Number(editingTask.priority),
                    dueDate: editingTask.due_date
                  }, {
                    onSuccess: () => setEditingTask(null)
                  });
                }} 
                loading={updateTaskMutation.isPending}
              >
                Save Changes
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}
