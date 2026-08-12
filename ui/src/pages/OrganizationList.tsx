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
  Textarea
} from '@mantine/core';
import { 
  Search, 
  Plus, 
  Building2, 
  Edit2, 
  Trash2, 
  Filter
} from 'lucide-react';
import { useOrganizations, useCreateOrganization, useUpdateOrganization, useDeleteOrganization } from '../hooks/useOrganization';
import { PageHeader } from '../components/PageHeader';
import { useState } from 'react';
import { notifications } from '@mantine/notifications';
import { failureMessage } from '../services/shared/errors';
import { TableLoadingState, ErrorState, EmptyState } from '../components/state';

export function OrganizationList() {
  const { data, isLoading, error, refetch } = useOrganizations();
  const createOrg = useCreateOrganization();
  const updateOrg = useUpdateOrganization();
  const deleteOrg = useDeleteOrganization();
  
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingOrg, setEditingOrg] = useState<any>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');


  const organizations = data?.organizations || [];

  const handleOpenModal = (org?: any) => {
    if (org) {
      setEditingOrg(org);
      setName(org.name);
      setDescription(org.description);
    } else {
      setEditingOrg(null);
      setName('');
      setDescription('');
    }
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      if (editingOrg) {
        await updateOrg.mutateAsync({ id: editingOrg.id, name, description });
        notifications.show({ title: 'Success', message: 'Organization updated successfully', color: 'green' });
      } else {
        await createOrg.mutateAsync({ name, description });
        notifications.show({ title: 'Success', message: 'Organization created successfully', color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error) {
      // Surface the actual reason rather than a generic string — the user
      // cannot tell a validation problem from an outage otherwise.
      notifications.show({ title: 'Error', message: failureMessage('Failed to save organization', error), color: 'red' });
    }
  };

  const handleDelete = async (id: string) => {
    if (window.confirm('Are you sure you want to delete this organization? All projects under this organization will be affected.')) {
      try {
        await deleteOrg.mutateAsync(id);
        notifications.show({ title: 'Success', message: 'Organization deleted successfully', color: 'green' });
      } catch {
        notifications.show({ title: 'Error', message: 'Failed to delete organization', color: 'red' });
      }
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Organizations" 
        description="Manage your organizations and their projects."
        actions={
          <Button 
            variant="filled" 
            color="indigo" 
            leftSection={<Plus size={16} />}
            onClick={() => handleOpenModal()}
          >
            New Organization
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput 
                placeholder="Search organizations..." 
                leftSection={<Search size={16} />} 
                style={{ flex: 1, maxWidth: 400 }}
                variant="filled"
                radius="md"
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
          <ErrorState error={error} action="load your organizations" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Organization Name</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {organizations.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={4}>
                    <EmptyState icon={Building2} title="No organizations yet" description="An organization is the top-level container for your projects and people." />
                  </Table.Td>
                </Table.Tr>
              ) : (
                organizations.map((org: any) => (
                  <Table.Tr key={org.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <Building2 size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{org.name}</Text>
                          <Text size="xs" c="dimmed">ID: {org.id}</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" lineClamp={1}>{org.description || 'No description'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Edit Organization">
                          <ActionIcon aria-label="Edit organization" 
                            variant="light" 
                            color="indigo" 
                            onClick={() => handleOpenModal(org)}
                          >
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete Organization">
                          <ActionIcon aria-label="Delete organization" 
                            variant="light" 
                            color="red"
                            onClick={() => handleDelete(org.id)}
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
            <Button onClick={handleSubmit} loading={createOrg.isPending || updateOrg.isPending}>
              {editingOrg ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
