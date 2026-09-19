import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Card,
  Group,
  Modal,
  MultiSelect,
  PasswordInput,
  Stack,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { Edit2, Plus, Search, Trash2, User, UserCircle } from 'lucide-react';
import { useState, useTransition } from 'react';

import { PageHeader } from '../components/PageHeader';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { MIN_PASSWORD_LENGTH } from '../domain/password';
import { isPrivilegedRole, ROLE_OPTIONS, roleLabel } from '../domain/roles';
import { matchesQuery } from '../domain/textSearch';
import { useOrganizations } from '../hooks/useOrganization';
import { useCreateUser, useDeleteUser, useUpdateUser, useUsers } from '../hooks/useUser';
import { errorMessage } from '../services/shared/errors';
import type { ApiOrganizationUser } from '../services/types';
import { useAppStore } from '../store/useAppStore';
import { useTranslation } from '../i18n/context';

const COLUMNS = 4;

export function UserList() {
  const { t } = useTranslation();
  const { data, isLoading, error, refetch } = useUsers();
  const { data: orgData } = useOrganizations();
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
  const [email, setEmail] = useState('');
  const [roles, setRoles] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [, startTransition] = useTransition();

  const allUsers = data?.users || [];
  const users = allUsers.filter((u) => matchesQuery(searchQuery, u.username, u.full_name, u.email));

  // New people join the organization being worked in. The form used to offer a
  // free-text "Organization" box, which named nothing the server could join.
  const currentOrganization = (orgData?.organizations ?? []).find((org) => org.id === currentOrganizationId);
  const organizationName = editingUser
    ? editingUser.organization?.name ?? currentOrganization?.name ?? ''
    : currentOrganization?.name ?? '';

  const passwordTooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const canSubmit = editingUser
    ? fullName.trim().length > 0
    : username.trim().length > 0 && fullName.trim().length > 0 && password.length >= MIN_PASSWORD_LENGTH;

  const handleOpenModal = (user?: ApiOrganizationUser) => {
    setEditingUser(user ?? null);
    setUsername(user?.username ?? '');
    setFullName(user?.full_name ?? '');
    setDisplayName(user?.display_name ?? '');
    setEmail(user?.email ?? '');
    setRoles(user?.roles ?? []);
    setPassword('');
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    try {
      if (editingUser) {
        await updateUser.mutateAsync({
          id: editingUser.id,
          full_name: fullName,
          display_name: displayName,
          organization: organizationName,
          email,
          roles,
        });
        notifications.show({ title: 'Saved', message: `${fullName} was updated.`, color: 'green' });
      } else {
        await createUser.mutateAsync({
          organization_id: currentOrganizationId ?? '',
          username,
          password,
          full_name: fullName,
          display_name: displayName,
          organization: organizationName,
          email,
          roles,
        });
        notifications.show({ title: 'Added', message: `${fullName} can sign in now.`, color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error: unknown) {
      notifications.show({ title: 'Could not save it', message: errorMessage(error, 'Failed to save the account'), color: 'red' });
    }
  };

  const handleDelete = async (user: ApiOrganizationUser) => {
    const who = user.full_name || user.username;
    const consequence =
      `Delete ${who}? Any tasks assigned to them stay open with nobody to do them — reassign those first. ` +
      'Their account cannot be recovered.';
    if (!window.confirm(consequence)) return;
    try {
      await deleteUser.mutateAsync(user.id);
      notifications.show({ title: 'Deleted', message: `${who} no longer has an account.`, color: 'green' });
    } catch (error: unknown) {
      notifications.show({ title: 'Could not delete them', message: errorMessage(error, 'Failed to delete the account'), color: 'red' });
    }
  };

  const handleSearchChange = (value: string) => {
    startTransition(() => setSearchQuery(value));
  };

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.platformAccess.title')}
        description={t('page.platformAccess.subtitle')}
        actions={
          <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => handleOpenModal()}>
            New account
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <TextInput
            aria-label="Search people"
            placeholder="Search by name, username or email…"
            leftSection={<Search size={16} />}
            style={{ maxWidth: 400 }}
            variant="filled"
            radius="md"
            onChange={(e) => handleSearchChange(e.currentTarget.value)}
          />
        </Box>

        {isLoading ? (
          <TableLoadingState rows={5} columns={COLUMNS} />
        ) : error ? (
          <ErrorState error={error} action="load the platform accounts" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Account</Table.Th>
                <Table.Th>Email</Table.Th>
                <Table.Th>Roles</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {users.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    {searchQuery ? (
                      <Text ta="center" c="dimmed" py="xl">Nobody matches “{searchQuery}”.</Text>
                    ) : (
                      <EmptyState icon={User} title="No accounts yet" description="Add the people who administer this installation. Process participants are managed separately, on the People page." />
                    )}
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
                        {(u.roles || []).map((role) => (
                          <Badge key={role} variant="light" size="sm" color={isPrivilegedRole(role) ? 'red' : 'blue'}>
                            {roleLabel(role)}
                          </Badge>
                        ))}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Edit account">
                          <ActionIcon aria-label={`Edit ${u.full_name || u.username}`} variant="light" color="indigo" onClick={() => handleOpenModal(u)}>
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete account">
                          <ActionIcon aria-label={`Delete ${u.full_name || u.username}`} variant="light" color="red" onClick={() => handleDelete(u)}>
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
        {/* The directory arrives whole; this says how much of it is on screen
            until it is paged. */}
        {!isLoading && !error && allUsers.length > 0 && (
          <Text size="xs" c="dimmed" px="md" py="sm">
            {searchQuery ? `${users.length} of ${allUsers.length} accounts match` : `Showing all ${allUsers.length} accounts`}
          </Text>
        )}
      </Card>

      <Modal
        opened={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={<Text fw={700}>{editingUser ? 'Edit account' : 'New account'}</Text>}
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
              description={`At least ${MIN_PASSWORD_LENGTH} characters`}
              placeholder="Enter password"
              required
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
              error={passwordTooShort ? `Needs at least ${MIN_PASSWORD_LENGTH} characters` : undefined}
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
            description={editingUser ? undefined : 'New people join the organization you are working in'}
            value={organizationName}
            disabled
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
            data={ROLE_OPTIONS.map(({ value, label, description }) => ({
              value,
              label: `${label} — ${description}`,
            }))}
            value={roles}
            onChange={setRoles}
          />
          <Group justify="flex-end" mt="md">
            <Button variant="light" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button onClick={handleSubmit} loading={createUser.isPending || updateUser.isPending} disabled={!canSubmit}>
              {editingUser ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
