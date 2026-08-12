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
  Modal,
  Box,
  Tooltip,
  Textarea,
  Select,
  MultiSelect,
  Badge,
} from '@mantine/core';
import {
  Search,
  Plus,
  ShieldCheck,
  Edit2,
  Trash2,
  Filter,
  UserPlus,
  UserMinus, Users
} from 'lucide-react';
import {
  useGroups,
  useCreateGroup,
  useUpdateGroup,
  useDeleteGroup,
  useGroupMembers,
  useAddMembership,
  useRemoveMembership,
  useUsers,
} from '../hooks/useUser';
import { PageHeader } from '../components/PageHeader';
import { useState, useTransition } from 'react';
import { notifications } from '@mantine/notifications';
import { useAppStore } from '../store/useAppStore';
import { TableLoadingState, ErrorState, EmptyState } from '../components/state';

const AVAILABLE_ROLES = ['admin', 'user', 'manager', 'developer', 'viewer'];

export function GroupList() {
  const { data, isLoading, error, refetch } = useGroups();
  const { data: usersData } = useUsers();
  const createGroup = useCreateGroup();
  const updateGroup = useUpdateGroup();
  const deleteGroup = useDeleteGroup();
  const addMembership = useAddMembership();
  const removeMembership = useRemoveMembership();
  const { currentOrganizationId } = useAppStore();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isMembersModalOpen, setIsMembersModalOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<any>(null);
  const [selectedGroup, setSelectedGroup] = useState<any>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [roles, setRoles] = useState<string[]>([]);
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [, startTransition] = useTransition();

  const { data: membersData, isLoading: membersLoading } = useGroupMembers(selectedGroup?.id || '');


  const allGroups = data?.groups || [];
  const groups = searchQuery
    ? allGroups.filter((g: any) =>
        g.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
        g.description?.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : allGroups;

  const allUsers = usersData?.users || [];
  const members = membersData?.users || [];
  const memberIds = new Set(members.map((m: any) => m.id));
  const availableUsers = allUsers
    .filter((u: any) => !memberIds.has(u.id))
    .map((u: any) => ({ value: u.id, label: `${u.fullName || u.username} (@${u.username})` }));

  const handleOpenModal = (group?: any) => {
    if (group) {
      setEditingGroup(group);
      setName(group.name || '');
      setDescription(group.description || '');
      setRoles(group.roles || []);
    } else {
      setEditingGroup(null);
      setName('');
      setDescription('');
      setRoles([]);
    }
    setIsModalOpen(true);
  };

  const handleOpenMembers = (group: any) => {
    setSelectedGroup(group);
    setSelectedUserId(null);
    setIsMembersModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      if (editingGroup) {
        await updateGroup.mutateAsync({ id: editingGroup.id, name, description, roles });
        notifications.show({ title: 'Success', message: 'Group updated successfully', color: 'green' });
      } else {
        await createGroup.mutateAsync({
          organization_id: currentOrganizationId || '',
          name,
          description,
          roles,
        });
        notifications.show({ title: 'Success', message: 'Group created successfully', color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error: any) {
      notifications.show({ title: 'Error', message: error.message || 'Failed to save group', color: 'red' });
    }
  };

  const handleDelete = async (id: string) => {
    if (window.confirm('Are you sure you want to delete this group? All memberships will be removed.')) {
      try {
        await deleteGroup.mutateAsync(id);
        notifications.show({ title: 'Success', message: 'Group deleted successfully', color: 'green' });
      } catch (error: any) {
        notifications.show({ title: 'Error', message: error.message || 'Failed to delete group', color: 'red' });
      }
    }
  };

  const handleAddMember = async () => {
    if (!selectedUserId || !selectedGroup) return;
    try {
      await addMembership.mutateAsync({ groupId: selectedGroup.id, userId: selectedUserId });
      setSelectedUserId(null);
      notifications.show({ title: 'Success', message: 'Member added successfully', color: 'green' });
    } catch (error: any) {
      notifications.show({ title: 'Error', message: error.message || 'Failed to add member', color: 'red' });
    }
  };

  const handleRemoveMember = async (userId: string) => {
    if (!selectedGroup) return;
    try {
      await removeMembership.mutateAsync({ groupId: selectedGroup.id, userId });
      notifications.show({ title: 'Success', message: 'Member removed successfully', color: 'green' });
    } catch (error: any) {
      notifications.show({ title: 'Error', message: error.message || 'Failed to remove member', color: 'red' });
    }
  };

  const handleSearchChange = (value: string) => {
    startTransition(() => {
      setSearchQuery(value);
    });
  };

  return (
    <Stack gap="xl">
      <PageHeader
        title="Groups"
        description="Manage user groups and memberships."
        actions={
          <Button
            variant="filled"
            color="indigo"
            leftSection={<Plus size={16} />}
            onClick={() => handleOpenModal()}
          >
            New Group
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput
                placeholder="Search groups..."
                leftSection={<Search size={16} />}
                style={{ flex: 1, maxWidth: 400 }}
                variant="filled"
                radius="md"
                onChange={(e) => handleSearchChange(e.currentTarget.value)}
              />
              <Button variant="light" leftSection={<Filter size={16} />} radius="md">Filter</Button>
            </Group>
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
          <ErrorState error={error} action="load your groups" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Group Name</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th>Assigned Roles</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {groups.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={4}>
                    <EmptyState icon={Users} title="No groups yet" description="Groups let you route work to a team rather than to one named person." />
                  </Table.Td>
                </Table.Tr>
              ) : (
                groups.map((g: any) => (
                  <Table.Tr key={g.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <ShieldCheck size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{g.name}</Text>
                          <Text size="xs" c="dimmed">ID: {g.id}</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" lineClamp={1}>{g.description || '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {(g.roles || []).map((role: string) => (
                          <Badge key={role} variant="light" size="sm" color="cyan">
                            {role}
                          </Badge>
                        ))}
                        {(g.roles || []).length === 0 && <Text size="xs" c="dimmed">No roles</Text>}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Manage Members">
                          <ActionIcon aria-label="Add member to group"
                            variant="light"
                            color="teal"
                            onClick={() => handleOpenMembers(g)}
                          >
                            <UserPlus size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Edit Group">
                          <ActionIcon aria-label="Edit group"
                            variant="light"
                            color="indigo"
                            onClick={() => handleOpenModal(g)}
                          >
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete Group">
                          <ActionIcon aria-label="Delete group"
                            variant="light"
                            color="red"
                            onClick={() => handleDelete(g.id)}
                          >
                            <Trash2 size={16} />
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
        )}
      </Card>

      {/* Create/Edit Group Modal */}
      <Modal
        opened={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={<Text fw={700}>{editingGroup ? 'Edit Group' : 'New Group'}</Text>}
        radius="lg"
      >
        <Stack gap="md">
          <TextInput
            label="Group Name"
            placeholder="Enter group name"
            required
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <Textarea
            label="Description"
            placeholder="Enter group description"
            minRows={3}
            value={description}
            onChange={(e) => setDescription(e.currentTarget.value)}
          />
          <MultiSelect
            label="Group Roles"
            description="Users in this group will inherit these roles."
            placeholder="Select roles"
            data={AVAILABLE_ROLES}
            value={roles}
            onChange={setRoles}
            clearable
            searchable
          />
          <Group justify="flex-end" mt="md">
            <Button variant="light" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button onClick={handleSubmit} loading={createGroup.isPending || updateGroup.isPending}>
              {editingGroup ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* Members Modal */}
      <Modal
        opened={isMembersModalOpen}
        onClose={() => setIsMembersModalOpen(false)}
        title={<Text fw={700}>Members of {selectedGroup?.name}</Text>}
        radius="lg"
        size="lg"
      >
        <Stack gap="md">
          <Group>
            <Select
              placeholder="Select a user to add"
              data={availableUsers}
              value={selectedUserId}
              onChange={setSelectedUserId}
              searchable
              style={{ flex: 1 }}
            />
            <Button
              leftSection={<UserPlus size={16} />}
              onClick={handleAddMember}
              disabled={!selectedUserId}
              loading={addMembership.isPending}
            >
              Add
            </Button>
          </Group>

          {membersLoading ? (
            <Text c="dimmed">Loading members...</Text>
          ) : members.length === 0 ? (
            <Text c="dimmed" ta="center" py="md">No members in this group yet.</Text>
          ) : (
            <Table verticalSpacing="sm" highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>User</Table.Th>
                  <Table.Th>Email</Table.Th>
                  <Table.Th ta="right">Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {members.map((m: any) => (
                  <Table.Tr key={m.id}>
                    <Table.Td>
                      <Stack gap={0}>
                        <Text fw={600} size="sm">{m.fullName || m.username}</Text>
                        <Text size="xs" c="dimmed">@{m.username}</Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{m.email || '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group justify="flex-end">
                        <Tooltip label="Remove Member">
                          <ActionIcon aria-label="Remove member from group"
                            variant="light"
                            color="red"
                            onClick={() => handleRemoveMember(m.id)}
                          >
                            <UserMinus size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Stack>
      </Modal>
    </Stack>
  );
}
