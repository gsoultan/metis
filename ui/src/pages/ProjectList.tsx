import {
  ActionIcon,
  Badge,
  Button,
  Card,
  Group,
  Modal,
  Select,
  Stack,
  Table,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { CheckCircle2, Edit2, ExternalLink, FolderGit2, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { PageHeader } from '../components/PageHeader';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { listDetails } from '../domain/listDetails';
import { useOrganizations } from '../hooks/useOrganization';
import { useCreateProject, useDeleteProject, useProjects, useUpdateProject } from '../hooks/useProcess';
import { failureMessage } from '../services/shared/errors';
import type { Project } from '../services/types';
import { useAppStore } from '../store/useAppStore';
import { useTranslation } from '../i18n/context';

const COLUMNS = 3;

export function ProjectList() {
  const { t } = useTranslation();
  const { currentProjectId, setCurrentProjectId, setCurrentOrganizationId, currentOrganizationId, expertMode } = useAppStore();
  const { data, isLoading, error, refetch } = useProjects(currentOrganizationId);
  const { data: orgData } = useOrganizations();
  const createProject = useCreateProject();
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [organizationId, setOrganizationId] = useState<string | null>(null);

  const projects = data?.projects || [];
  const organizations = orgData?.organizations || [];

  const handleOpenModal = (project?: Project) => {
    setEditingProject(project ?? null);
    setName(project?.name ?? '');
    setDescription(project?.description ?? '');
    // The project nests its organization; there is no organization_id. Reading
    // one left the picker empty on an edit, and saving that would have moved
    // the project out of its organization.
    setOrganizationId(project ? project.organization?.id ?? null : organizations[0]?.id ?? null);
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    if (!organizationId || !name.trim()) return;
    try {
      if (editingProject) {
        await updateProject.mutateAsync({ projectId: editingProject.id, organizationId, name, description });
        notifications.show({ title: 'Saved', message: `${name} was updated.`, color: 'green' });
      } else {
        await createProject.mutateAsync({ organizationId, name, description });
        notifications.show({ title: 'Created', message: `${name} is ready.`, color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error) {
      // Surface the actual reason rather than a generic string — the user
      // cannot tell a validation problem from an outage otherwise.
      notifications.show({ title: 'Could not save it', message: failureMessage('Failed to save project', error), color: 'red' });
    }
  };

  const handleDelete = async (project: Project) => {
    const consequence =
      `Delete ${project.name}? Its process models, decisions, running instances and open tasks are all lost. ` +
      'This cannot be undone.';
    if (!window.confirm(consequence)) return;
    try {
      await deleteProject.mutateAsync(project.id);
      notifications.show({ title: 'Deleted', message: `${project.name} is gone.`, color: 'green' });
      if (currentProjectId === project.id) {
        setCurrentProjectId(null);
      }
    } catch (error) {
      notifications.show({ title: 'Could not delete it', message: failureMessage('Failed to delete project', error), color: 'red' });
    }
  };

  const select = (project: Project) => {
    setCurrentProjectId(project.id);
    // Selecting a project set the current organization to undefined, after
    // which every organization-scoped query — users, groups, connectors — had
    // nothing to scope to.
    setCurrentOrganizationId(project.organization?.id ?? null);
  };

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.projects.title')}
        description={t('page.projects.subtitle')}
        actions={
          <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => handleOpenModal()}>
            New Project
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        {/*
          Loading and error render inside the page, not instead of it, so the
          layout does not jump when data arrives and a failed request is not
          mistaken for an empty list.
        */}
        {isLoading ? (
          <TableLoadingState rows={5} columns={COLUMNS} />
        ) : error ? (
          <ErrorState error={error} action="load your projects" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Project</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {projects.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    <EmptyState
                      icon={FolderGit2}
                      title="No projects yet"
                      description="Projects group related process models, decisions and tasks. Create one to get started."
                      action={<Button onClick={() => handleOpenModal()} leftSection={<Plus size={16} />}>Create a project</Button>}
                    />
                  </Table.Td>
                </Table.Tr>
              ) : (
                projects.map((project) => {
                  const selected = currentProjectId === project.id;
                  const details = listDetails(expertMode, project.organization?.name, project.id);
                  return (
                    <Table.Tr key={project.id}>
                      <Table.Td>
                        <Group gap="sm">
                          <ThemeIcon color={selected ? 'green' : 'indigo'} variant="light" radius="md">
                            <FolderGit2 size={16} />
                          </ThemeIcon>
                          <Stack gap={0}>
                            <Group gap="xs">
                              <Text fw={700} size="sm">{project.name}</Text>
                              {/* "Active" suggested the project was live somewhere.
                                  It is only which one this browser is looking at. */}
                              {selected && (
                                <Badge size="xs" variant="light" color="green" leftSection={<CheckCircle2 size={10} />}>
                                  Selected in this browser
                                </Badge>
                              )}
                            </Group>
                            {details.detail !== undefined && (
                              <Text size="xs" c="dimmed">{details.detail}</Text>
                            )}
                            {details.id !== undefined && (
                              <Text size="xs" c="dimmed" ff="monospace">{details.id}</Text>
                            )}
                          </Stack>
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" lineClamp={1}>{project.description || 'No description'}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Group gap="xs" justify="flex-end">
                          <Button
                            size="xs"
                            variant={selected ? 'filled' : 'light'}
                            color="green"
                            leftSection={<ExternalLink size={14} />}
                            onClick={() => select(project)}
                          >
                            {selected ? 'Selected' : 'Select'}
                          </Button>
                          <Tooltip label="Edit project">
                            <ActionIcon aria-label={`Edit ${project.name}`} variant="light" color="indigo" onClick={() => handleOpenModal(project)}>
                              <Edit2 size={16} />
                            </ActionIcon>
                          </Tooltip>
                          <Tooltip label="Delete project">
                            <ActionIcon aria-label={`Delete ${project.name}`} variant="light" color="red" onClick={() => handleDelete(project)}>
                              <Trash2 size={16} />
                            </ActionIcon>
                          </Tooltip>
                        </Group>
                      </Table.Td>
                    </Table.Tr>
                  );
                })
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
        )}
      </Card>

      <Modal
        opened={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={<Text fw={700}>{editingProject ? 'Edit Project' : 'New Project'}</Text>}
        radius="lg"
      >
        <Stack gap="md">
          <Select
            label="Organization"
            placeholder="Select organization"
            required
            data={organizations.map((org) => ({ value: org.id, label: org.name }))}
            value={organizationId}
            onChange={setOrganizationId}
          />
          <TextInput
            label="Project Name"
            placeholder="Enter project name"
            required
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <Textarea
            label="Description"
            placeholder="Enter project description"
            minRows={3}
            value={description}
            onChange={(e) => setDescription(e.currentTarget.value)}
          />
          <Group justify="flex-end" mt="md">
            <Button variant="light" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button
              onClick={handleSubmit}
              loading={createProject.isPending || updateProject.isPending}
              disabled={!name.trim() || !organizationId}
            >
              {editingProject ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
