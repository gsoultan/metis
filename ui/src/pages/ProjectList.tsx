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
  Badge, 
  Modal, 
  Box,
  Tooltip,
  Textarea
} from '@mantine/core';
import { 
  Search, 
  Plus, 
  FolderGit2, 
  Edit2, 
  Trash2, 
  CheckCircle2,
  Filter,
  ExternalLink
} from 'lucide-react';
import { useProjects, useCreateProject, useUpdateProject, useDeleteProject } from '../hooks/useProcess';
import { useOrganizations } from '../hooks/useOrganization';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { useState } from 'react';
import { notifications } from '@mantine/notifications';
import { Select } from '@mantine/core';
import { failureMessage } from '../services/shared/errors';
import { TableLoadingState, ErrorState, EmptyState } from '../components/state';

export function ProjectList() {
  const { currentProjectId, setCurrentProjectId, setCurrentOrganizationId, currentOrganizationId } = useAppStore();
  const { data, isLoading, error, refetch } = useProjects(currentOrganizationId);
  const { data: orgData } = useOrganizations();
  const createProject = useCreateProject();
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();
  
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<any>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [organizationId, setOrganizationId] = useState<string | null>(null);


  const projects = data?.projects || [];
  const organizations = orgData?.organizations || [];

  const handleOpenModal = (project?: any) => {
    if (project) {
      setEditingProject(project);
      setName(project.name);
      setDescription(project.description);
      setOrganizationId(project.organization_id);
    } else {
      setEditingProject(null);
      setName('');
      setDescription('');
      setOrganizationId(organizations.length > 0 ? organizations[0].id : null);
    }
    setIsModalOpen(true);
  };

  const handleSubmit = async () => {
    if (!organizationId) {
      notifications.show({ title: 'Error', message: 'Please select an organization', color: 'red' });
      return;
    }
    try {
      if (editingProject) {
        await updateProject.mutateAsync({ projectId: editingProject.id, organizationId, name, description });
        notifications.show({ title: 'Success', message: 'Project updated successfully', color: 'green' });
      } else {
        await createProject.mutateAsync({ organizationId, name, description });
        notifications.show({ title: 'Success', message: 'Project created successfully', color: 'green' });
      }
      setIsModalOpen(false);
    } catch (error) {
      // Surface the actual reason rather than a generic string — the user
      // cannot tell a validation problem from an outage otherwise.
      notifications.show({ title: 'Error', message: failureMessage('Failed to save project', error), color: 'red' });
    }
  };

  const handleDelete = async (id: string) => {
    if (window.confirm('Are you sure you want to delete this project?')) {
      try {
        await deleteProject.mutateAsync(id);
        notifications.show({ title: 'Success', message: 'Project deleted successfully', color: 'green' });
        if (currentProjectId === id) {
          setCurrentProjectId(null);
        }
      } catch {
        notifications.show({ title: 'Error', message: 'Failed to delete project', color: 'red' });
      }
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Projects" 
        description="Organize your processes and tasks into projects."
        actions={
          <Button 
            variant="filled" 
            color="indigo" 
            leftSection={<Plus size={16} />}
            onClick={() => handleOpenModal()}
          >
            New Project
          </Button>
        }
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput 
                placeholder="Search projects..." 
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
          Loading and error render inside the page, not instead of it. The
          previous `if (isLoading) return <Text>Loading projects...</Text>`
          replaced the entire page — title, search, actions — with one line, so
          the layout jumped when data arrived, and a failed request was
          indistinguishable from an empty list.
        */}
        {isLoading ? (
          <TableLoadingState rows={5} columns={4} />
        ) : error ? (
          <ErrorState error={error} action="load your projects" onRetry={() => refetch()} />
        ) : (
        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Project Name</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {projects.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={4}>
                    <EmptyState
                      icon={FolderGit2}
                      title="No projects yet"
                      description="Projects group related process models, decisions and tasks. Create one to get started."
                      action={<Button onClick={() => handleOpenModal()} leftSection={<Plus size={16} />}>Create a project</Button>}
                    />
                  </Table.Td>
                </Table.Tr>
              ) : (
                projects.map((project: any) => (
                  <Table.Tr key={project.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color={currentProjectId === project.id ? "green" : "indigo"} variant="light" radius="md">
                          <FolderGit2 size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{project.name}</Text>
                          <Text size="xs" c="dimmed">ID: {project.id}</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" lineClamp={1}>{project.description || 'No description'}</Text>
                    </Table.Td>
                    <Table.Td>
                      {currentProjectId === project.id ? (
                        <Badge variant="filled" color="green" leftSection={<CheckCircle2 size={12} />}>Active</Badge>
                      ) : (
                        <Badge variant="light" color="gray">Inactive</Badge>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Button 
                          size="xs" 
                          variant={currentProjectId === project.id ? "filled" : "light"}
                          color="green"
                          leftSection={<ExternalLink size={14} />}
                          onClick={() => {
                            setCurrentProjectId(project.id);
                            setCurrentOrganizationId(project.organization_id);
                          }}
                        >
                          Select
                        </Button>
                        <Tooltip label="Edit Project">
                          <ActionIcon aria-label="Edit project" 
                            variant="light" 
                            color="indigo" 
                            onClick={() => handleOpenModal(project)}
                          >
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete Project">
                          <ActionIcon aria-label="Delete project" 
                            variant="light" 
                            color="red"
                            onClick={() => handleDelete(project.id)}
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
        title={<Text fw={700}>{editingProject ? 'Edit Project' : 'New Project'}</Text>}
        radius="lg"
      >
        <Stack gap="md">
          <Select
            label="Organization"
            placeholder="Select organization"
            required
            data={organizations.map((org: any) => ({ value: org.id, label: org.name }))}
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
            <Button onClick={handleSubmit} loading={createProject.isPending || updateProject.isPending}>
              {editingProject ? 'Update' : 'Create'}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
