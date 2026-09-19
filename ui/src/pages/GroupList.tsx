import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Card,
  Group,
  Modal,
  MultiSelect,
  Stack,
  Table,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { Edit2, Plus, Search, ShieldCheck, Trash2, UserPlus, Users } from 'lucide-react';
import { useState, useTransition } from 'react';

import { PageHeader } from '../components/PageHeader';
import { MembersModal } from '../components/groups/MembersModal';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { ROLE_OPTIONS, roleLabel } from '../domain/roles';
import { matchesQuery } from '../domain/textSearch';
import { useCreateGroup, useDeleteGroup, useGroups, useUpdateGroup } from '../hooks/useUser';
import { errorMessage } from '../services/shared/errors';
import type { ApiGroup } from '../services/types';
import { useAppStore } from '../store/useAppStore';
import { useTranslation } from '../i18n/context';

const COLUMNS = 4;

export function GroupList() {
  const { t } = useTranslation();
  const { data, isLoading, error, refetch } = useGroups();
  const createGroup = useCreateGroup();
  const updateGroup = useUpdateGroup();
  const deleteGroup = useDeleteGroup();
  const { currentOrganizationId } = useAppStore();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<ApiGroup | null>(null);
  const [membersOf, setMembersOf] = useState<ApiGroup | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [roles, setRoles] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [, startTransition] = useTransition();

  const allGroups = data?.groups || [];
  const groups = allGroups.filter((g) => matchesQuery(searchQuery, g.name, g.description));

  const handleOpenModal = (group?: ApiGroup) => {
    setEditingGroup(group ?? null);
    setName(group?.name ?? '');
    setDescription(group?.description ?? '');
    setRoles(group?.roles ?? []);
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    if (!name.trim()) return;
    try {
      if (editingGroup) {
        await updateGroup.mutateAsync({ id: editingGroup.id, name, description, roles });
        notifications.show({ title: 'Saved', message: `${name} was updated.`, color: 'green' });
      } else {
        await createGroup.mutateAsync({ organization_id: currentOrganizationId || '', name, description, roles });
        notifications.show({ title: 'Created', message: `${name} is ready for members.`, color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error: unknown) {
      notifications.show({ title: 'Could not save it', message: errorMessage(error, 'Failed to save group'), color: 'red' });
    }
  };

  const handleDelete = async (group: ApiGroup) => {
    const consequence =
      `Delete ${group.name}? Its members lose the roles it gave them, and tasks routed to this group have nowhere to go. ` +
      'This cannot be undone.';
    if (!window.confirm(consequence)) return;
    try {
      await deleteGroup.mutateAsync(group.id);
      notifications.show({ title: 'Deleted', message: `${group.name} is gone.`, color: 'green' });
    } catch (error: unknown) {
      notifications.show({ title: 'Could not delete it', message: errorMessage(error, 'Failed to delete group'), color: 'red' });
    }
  };

  const handleSearchChange = (value: string) => {
    startTransition(() => setSearchQuery(value));
  };

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.groups.title')}
        description={t('page.groups.subtitle')}
        actions={
          <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => handleOpenModal()}>
            New Group
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <TextInput
            aria-label="Search groups"
            placeholder="Search groups…"
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
          <ErrorState error={error} action="load your groups" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Group</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th>Roles its members get</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {groups.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    {searchQuery ? (
                      <Text ta="center" c="dimmed" py="xl">No group matches “{searchQuery}”.</Text>
                    ) : (
                      <EmptyState icon={Users} title="No groups yet" description="Groups let you route work to a team rather than to one named person." />
                    )}
                  </Table.Td>
                </Table.Tr>
              ) : (
                groups.map((g) => (
                  <Table.Tr key={g.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <ShieldCheck size={16} />
                        </ThemeIcon>
                        <Text fw={700} size="sm">{g.name}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" lineClamp={1}>{g.description || '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {(g.roles ?? []).map((role) => (
                          <Badge key={role} variant="light" size="sm" color="cyan">{roleLabel(role)}</Badge>
                        ))}
                        {(g.roles ?? []).length === 0 && <Text size="xs" c="dimmed">None</Text>}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Manage members">
                          <ActionIcon aria-label={`Manage members of ${g.name}`} variant="light" color="teal" onClick={() => setMembersOf(g)}>
                            <UserPlus size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Edit group">
                          <ActionIcon aria-label={`Edit ${g.name}`} variant="light" color="indigo" onClick={() => handleOpenModal(g)}>
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete group">
                          <ActionIcon aria-label={`Delete ${g.name}`} variant="light" color="red" onClick={() => handleDelete(g)}>
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
            label="Roles its members get"
            description="Everyone in this group holds these roles as well as their own."
            placeholder="Select roles"
            data={ROLE_OPTIONS.map((option) => ({
              value: option.value,
              label: `${option.label} — ${option.description}`,
            }))}
            value={roles}
            onChange={setRoles}
            clearable
            searchable
          />
          <Group justify="flex-end" mt="md">
            <Button variant="light" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button onClick={handleSubmit} loading={createGroup.isPending || updateGroup.isPending} disabled={!name.trim()}>
              {editingGroup ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>

      <MembersModal group={membersOf} opened={membersOf !== null} onClose={() => setMembersOf(null)} />
    </Stack>
  );
}
