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
  Badge,
  MultiSelect,
  PasswordInput,
} from '@mantine/core';
import {
  Search,
  Plus,
  UserCircle,
  Edit2,
  Trash2,
  Filter, User
} from 'lucide-react';
import { useUsers, useCreateUser, useUpdateUser, useDeleteUser } from '../hooks/useUser';
import { PageHeader } from '../components/PageHeader';
import { useState, useTransition } from 'react';
import { notifications } from '@mantine/notifications';
import { useAppStore } from '../store/useAppStore';
import { TableLoadingState, ErrorState, EmptyState } from '../components/state';
import type { ApiOrganizationUser } from '../services/types';

/** A caught value is `unknown`; take its message when it has one. */
function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

const AVAILABLE_ROLES = ['admin', 'user', 'manager', 'developer'];

export function UserList() {
  const { data, isLoading, error, refetch } = useUsers();
  const createUser = useCreateUser();
  const updateUser = useUpdateUser();
  const deleteUser = useDeleteUser();
  const { currentOrganizationId } = useAppStore();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<ApiOrganizationUser | null>(null);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [fullName, setFullName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [organization, setOrganization] = useState('');
  const [email, setEmail] = useState('');
  const [roles, setRoles] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [, startTransition] = useTransition();


  const allUsers = data?.users || [];
  const users = searchQuery
    ? allUsers.filter((u) =>
        u.username?.toLowerCase().includes(searchQuery.toLowerCase()) ||
        u.full_name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
        u.email?.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : allUsers;

  const handleOpenModal = (user?: ApiOrganizationUser) => {
    if (user) {
      setEditingUser(user);
      setUsername(user.username || '');
      setFullName(user.full_name || '');
      setDisplayName(user.display_name || '');
      setOrganization(user.organization?.name || '');
      setEmail(user.email || '');
      setRoles(user.roles || []);
      setPassword('');
    } else {
      setEditingUser(null);
      setUsername('');
      setFullName('');
      setDisplayName('');
      setOrganization('');
      setEmail('');
      setRoles([]);
      setPassword('');
    }
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      if (editingUser) {
        await updateUser.mutateAsync({
          id: editingUser.id,
          full_name: fullName,
          display_name: displayName,
          organization,
          email,
          roles,
        });
        notifications.show({ title: 'Success', message: 'User updated successfully', color: 'green' });
      } else {
        await createUser.mutateAsync({
          organization_id: currentOrganizationId || '',
          username,
          password,
          full_name: fullName,
          display_name: displayName,
          organization,
          email,
          roles,
        });
        notifications.show({ title: 'Success', message: 'User created successfully', color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error: unknown) {
      notifications.show({ title: 'Error', message: errorMessage(error, 'Failed to save user'), color: 'red' });
    }
  };

  const handleDelete = async (id: string) => {
    if (window.confirm('Are you sure you want to delete this user? This action cannot be undone.')) {
      try {
        await deleteUser.mutateAsync(id);
        notifications.show({ title: 'Success', message: 'User deleted successfully', color: 'green' });
      } catch (error: unknown) {
        notifications.show({ title: 'Error', message: errorMessage(error, 'Failed to delete user'), color: 'red' });
      }
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
        title="Users"
        description="Manage users in your organization."
        actions={
          <Button
            variant="filled"
            color="indigo"
            leftSection={<Plus size={16} />}
            onClick={() => handleOpenModal()}
          >
            New User
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput
                placeholder="Search users..."
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
          <ErrorState error={error} action="load your users" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>User</Table.Th>
                <Table.Th>Email</Table.Th>
                <Table.Th>Roles</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {users.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={4}>
                    <EmptyState icon={User} title="No people yet" description="Invite people so they can be assigned tasks and manage processes." />
                  </Table.Td>
                </Table.Tr>
              ) : (
                users.map((u) => (
                  <Table.Tr key={u.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <UserCircle size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{u.full_name || u.username}</Text>
                          <Text size="xs" c="dimmed">@{u.username}</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{u.email || '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {(u.roles || []).map((role: string) => (
                          <Badge key={role} variant="light" size="sm" color={role === 'admin' ? 'red' : 'blue'}>
                            {role}
                          </Badge>
                        ))}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Edit User">
                          <ActionIcon aria-label="Edit user"
                            variant="light"
                            color="indigo"
                            onClick={() => handleOpenModal(u)}
                          >
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete User">
                          <ActionIcon aria-label="Delete user"
                            variant="light"
                            color="red"
                            onClick={() => handleDelete(u.id)}
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

      <Modal
        opened={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={<Text fw={700}>{editingUser ? 'Edit User' : 'New User'}</Text>}
        radius="lg"
      >
        <Stack gap="md">
          <TextInput
            label="Username"
            placeholder="Enter username"
            required
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
            disabled={!!editingUser}
          />
          {!editingUser && (
            <PasswordInput
              label="Password"
              placeholder="Enter password"
              required
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
            />
          )}
          <TextInput
            label="Full Name"
            placeholder="Enter full name"
            required
            value={fullName}
            onChange={(e) => setFullName(e.currentTarget.value)}
          />
          <TextInput
            label="Display Name"
            placeholder="Enter display name"
            value={displayName}
            onChange={(e) => setDisplayName(e.currentTarget.value)}
          />
          <TextInput
            label="Organization"
            placeholder="Enter organization"
            value={organization}
            onChange={(e) => setOrganization(e.currentTarget.value)}
          />
          <TextInput
            label="Email"
            placeholder="Enter email address"
            value={email}
            onChange={(e) => setEmail(e.currentTarget.value)}
          />
          <MultiSelect
            label="Roles"
            placeholder="Select roles"
            data={AVAILABLE_ROLES}
            value={roles}
            onChange={setRoles}
          />
          <Group justify="flex-end" mt="md">
            <Button variant="light" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button onClick={handleSubmit} loading={createUser.isPending || updateUser.isPending}>
              {editingUser ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
