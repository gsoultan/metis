import {
  ActionIcon,
  Box,
  Button,
  Card,
  Group,
  Stack,
  Table,
  Tabs,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useNavigate } from '@tanstack/react-router';
import { Edit2, Plus, Search, ShieldCheck, Trash2, User, UserCircle } from 'lucide-react';
import { useState, useTransition } from 'react';

import { AccountDialog } from '../components/access/AccountDialog';
import { AccountRoleBadges } from '../components/access/AccountRoleBadges';
import { RoleMatrix } from '../components/access/RoleMatrix';
import { PageHeader } from '../components/PageHeader';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { matchesQuery } from '../domain/textSearch';
import { useOrganizations } from '../hooks/useOrganization';
import { useDeleteUser, useUsers } from '../hooks/useUser';
import { errorMessage } from '../services/shared/errors';
import type { ApiOrganizationUser } from '../services/types';
import { useAppStore } from '../store/useAppStore';
import { useTranslation } from '../i18n/context';
import { Route } from '../routes/_authenticated.users';

const COLUMNS = 4;

/**
 * The dialog while it is open: the account it is for, or null for a new one.
 * It is mounted only while open, so each opening starts from the account it
 * was opened for.
 */
interface OpenDialog {
  account: ApiOrganizationUser | null;
}

export function UserList() {
  const { t } = useTranslation();
  const navigate = useNavigate({ from: Route.fullPath });
  const { tab } = Route.useSearch();
  const { data, isLoading, error, refetch } = useUsers();
  const { data: orgData } = useOrganizations();
  const deleteUser = useDeleteUser();
  const { currentOrganizationId } = useAppStore();

  const [dialog, setDialog] = useState<OpenDialog | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [, startTransition] = useTransition();

  const allUsers = data?.users || [];
  const users = allUsers.filter((u) => matchesQuery(searchQuery, u.username, u.full_name, u.email));

  // New people join the organization being worked in. The form used to offer a
  // free-text "Organization" box, which named nothing the server could join.
  const currentOrganization = (orgData?.organizations ?? []).find((org) => org.id === currentOrganizationId);
  const organization = { id: currentOrganizationId ?? '', name: currentOrganization?.name ?? '' };

  const openDialog = (account: ApiOrganizationUser | null) => setDialog({ account });

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
          <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => openDialog(null)}>
            New account
          </Button>
        }
      />

      {/* Two views of the same accounts: each one's details, or who holds
          which role. Only the open one is rendered, so the role matrix asks
          for nothing until somebody opens it. */}
      <Tabs
        value={tab}
        onChange={(value) => navigate({ search: { tab: value === 'roles' ? 'roles' : 'accounts' } })}
        variant="outline"
        radius="md"
        keepMounted={false}
      >
        <Tabs.List mb="md">
          <Tabs.Tab value="accounts" leftSection={<UserCircle size={16} />}>
            {t('access.tabAccounts')}
          </Tabs.Tab>
          <Tabs.Tab value="roles" leftSection={<ShieldCheck size={16} />}>
            {t('access.tabRoles')}
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="accounts">
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
                          <AccountRoleBadges account={u} />
                        </Table.Td>
                        <Table.Td>
                          <Group gap="xs" justify="flex-end">
                            <Tooltip label="Edit account">
                              <ActionIcon aria-label={`Edit ${u.full_name || u.username}`} variant="light" color="indigo" onClick={() => openDialog(u)}>
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
        </Tabs.Panel>
        <Tabs.Panel value="roles">
          <RoleMatrix />
        </Tabs.Panel>
      </Tabs>

      {dialog && (
        <AccountDialog
          opened
          onClose={() => setDialog(null)}
          account={dialog.account}
          organization={organization}
        />
      )}
    </Stack>
  );
}
