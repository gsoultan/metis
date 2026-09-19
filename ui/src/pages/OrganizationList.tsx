import {
  ActionIcon,
  Button,
  Card,
  Group,
  Modal,
  Stack,
  Table,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { Building2, Edit2, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { PageHeader } from '../components/PageHeader';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import type { Organization } from '../gen/entities/organization_pb';
import { useCreateOrganization, useDeleteOrganization, useOrganizations, useUpdateOrganization } from '../hooks/useOrganization';
import { failureMessage } from '../services/shared/errors';
import { useAppStore } from '../store/useAppStore';
import { useTranslation } from '../i18n/context';

const COLUMNS = 3;

export function OrganizationList() {
  const { t } = useTranslation();
  const { data, isLoading, error, refetch } = useOrganizations();
  const { expertMode } = useAppStore();
  const createOrg = useCreateOrganization();
  const updateOrg = useUpdateOrganization();
  const deleteOrg = useDeleteOrganization();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingOrg, setEditingOrg] = useState<Organization | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');

  const organizations = data?.organizations || [];

  const handleOpenModal = (org?: Organization) => {
    setEditingOrg(org ?? null);
    setName(org?.name ?? '');
    setDescription(org?.description ?? '');
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    if (!name.trim()) return;
    try {
      if (editingOrg) {
        await updateOrg.mutateAsync({ id: editingOrg.id, name, description });
        notifications.show({ title: 'Saved', message: `${name} was updated.`, color: 'green' });
      } else {
        await createOrg.mutateAsync({ name, description });
        notifications.show({ title: 'Created', message: `${name} is ready.`, color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error) {
      // Surface the actual reason rather than a generic string — the user
      // cannot tell a validation problem from an outage otherwise.
      notifications.show({ title: 'Could not save it', message: failureMessage('Failed to save organization', error), color: 'red' });
    }
  };

  const handleDelete = async (org: Organization) => {
    const consequence =
      `Delete ${org.name}? Every project in it — with their process models, decisions, running instances and open ` +
      'tasks — is lost, and its people lose their memberships. This cannot be undone.';
    if (!window.confirm(consequence)) return;
    try {
      await deleteOrg.mutateAsync(org.id);
      notifications.show({ title: 'Deleted', message: `${org.name} is gone.`, color: 'green' });
    } catch (error) {
      notifications.show({ title: 'Could not delete it', message: failureMessage('Failed to delete organization', error), color: 'red' });
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.organizations.title')}
        description={t('page.organizations.subtitle')}
        actions={
          <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => handleOpenModal()}>
            New Organization
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        {/*
          Loading and error render inside the page rather than replacing it, so
          the layout does not jump when data arrives and a failed request is
          not mistaken for an empty list.
        */}
        {isLoading ? (
          <TableLoadingState rows={5} columns={COLUMNS} />
        ) : error ? (
          <ErrorState error={error} action="load your organizations" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Organization</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {organizations.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    <EmptyState
                      icon={Building2}
                      title="No organizations yet"
                      description="An organization is the top-level container for your projects and people."
                      action={<Button onClick={() => handleOpenModal()} leftSection={<Plus size={16} />}>Create an organization</Button>}
                    />
                  </Table.Td>
                </Table.Tr>
              ) : (
                organizations.map((org) => (
                  <Table.Tr key={org.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <Building2 size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{org.name}</Text>
                          {expertMode && <Text size="xs" c="dimmed" ff="monospace">{org.id}</Text>}
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" lineClamp={1}>{org.description || 'No description'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Edit organization">
                          <ActionIcon aria-label={`Edit ${org.name}`} variant="light" color="indigo" onClick={() => handleOpenModal(org)}>
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete organization">
                          <ActionIcon aria-label={`Delete ${org.name}`} variant="light" color="red" onClick={() => handleDelete(org)}>
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
        title={<Text fw={700}>{editingOrg ? 'Edit Organization' : 'New Organization'}</Text>}
        radius="lg"
      >
        <Stack gap="md">
          <TextInput
            label="Organization Name"
            placeholder="Enter organization name"
            required
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <Textarea
            label="Description"
            placeholder="Enter organization description"
            minRows={3}
            value={description}
            onChange={(e) => setDescription(e.currentTarget.value)}
          />
          <Group justify="flex-end" mt="md">
            <Button variant="light" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button onClick={handleSubmit} loading={createOrg.isPending || updateOrg.isPending} disabled={!name.trim()}>
              {editingOrg ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
