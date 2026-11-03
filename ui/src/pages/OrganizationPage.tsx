import {
  ActionIcon,
  Anchor,
  Badge,
  Button,
  Card,
  Group,
  SimpleGrid,
  Stack,
  Table,
  Tabs,
  Text,
  Textarea,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { Link, useNavigate } from '@tanstack/react-router';
import { ArrowLeft, Building2, Edit2, FolderGit2, LayoutDashboard, Plus, Settings, Users } from 'lucide-react';
import { useState } from 'react';

import { AccountDialog } from '../components/access/AccountDialog';
import { AccountRoleBadges } from '../components/access/AccountRoleBadges';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import { hasRole } from '../domain/access';
import { organizationInitials, organizationSummary } from '../domain/organizationSummary';
import { PRIVILEGED_ROLE } from '../domain/roles';
import {
  useDeleteOrganization,
  useOrganizations,
  useSwitchOrganization,
  useUpdateOrganization,
} from '../hooks/useOrganization';
import { useProjects } from '../hooks/useProjects';
import { useUsers } from '../hooks/useUser';
import { useViewerAccess } from '../hooks/useViewerAccess';
import { Route } from '../routes/_authenticated.organizations_.$organizationId';
import { failureMessage } from '../services/shared/errors';
import type { ApiOrganizationUser } from '../services/types';
import { useAppStore } from '../store/useAppStore';

type Tab = 'overview' | 'members' | 'projects' | 'settings';

/**
 * One organization: what is in it, who is in it, and its settings.
 *
 * The server answers for the organization a request is for, so the page shows
 * an organization only while it is the one being worked in. Opening another
 * offers to switch to it rather than showing the current one's people under
 * the other's name.
 */
export function OrganizationPage() {
  const { organizationId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const currentOrganizationId = useAppStore((state) => state.currentOrganizationId);
  const switchOrganization = useSwitchOrganization();
  const { data: orgData, isLoading: orgsLoading, error: orgsError, refetch } = useOrganizations();

  const organization = (orgData?.organizations ?? []).find((o) => o.id === organizationId);

  if (orgsLoading) return <TableLoadingState rows={4} columns={2} />;
  if (orgsError) return <ErrorState error={orgsError} action="load your organizations" onRetry={() => refetch()} />;

  if (!organization) {
    return (
      <Stack gap="lg">
        <BackLink />
        <EmptyState
          icon={Building2}
          title="Not one of your organizations"
          description="It does not exist, or you are not a member of it. Your organizations are listed on the Organizations page."
        />
      </Stack>
    );
  }

  if (organization.id !== currentOrganizationId) {
    return (
      <Stack gap="lg">
        <BackLink />
        <EmptyState
          icon={Building2}
          title={`You are working in another organization`}
          description={`Switch to ${organization.name} to see its members, projects and settings.`}
          action={<Button onClick={() => switchOrganization(organization.id)}>Switch to {organization.name}</Button>}
        />
      </Stack>
    );
  }

  return (
    <CurrentOrganization
      organization={organization}
      tab={tab}
      onTab={(next) => void navigate({ search: { tab: next } })}
    />
  );
}

function BackLink() {
  return (
    <Anchor component={Link} to="/organizations" size="sm">
      <Group gap={6} wrap="nowrap">
        <ArrowLeft size={14} aria-hidden />
        All organizations
      </Group>
    </Anchor>
  );
}

interface CurrentOrganizationProps {
  organization: { id: string; name: string; description: string };
  tab: Tab;
  onTab: (tab: Tab) => void;
}

function CurrentOrganization({ organization, tab, onTab }: CurrentOrganizationProps) {
  const viewer = useViewerAccess();
  const isAdministrator = hasRole(viewer, PRIVILEGED_ROLE);
  const members = useUsers();
  const projects = useProjects(organization.id);

  const memberCount = members.data ? (members.data.users ?? []).length : undefined;
  const projectCount = projects.data ? (projects.data.projects ?? []).length : undefined;

  return (
    <Stack gap="lg">
      <BackLink />
      <Group gap="md" wrap="nowrap" align="center">
        <div
          aria-hidden
          style={{
            width: 48,
            height: 48,
            flex: 'none',
            borderRadius: 10,
            background: 'var(--mantine-color-indigo-6)',
            color: 'white',
            fontWeight: 700,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          {organizationInitials(organization.name)}
        </div>
        <Stack gap={2} style={{ minWidth: 0 }}>
          <Group gap="xs">
            <Title order={2} size="h3">{organization.name}</Title>
            <Badge size="sm" variant="light" color="green">Working in</Badge>
          </Group>
          <Text size="sm" c="dimmed">{organizationSummary(memberCount, projectCount, viewer.roles)}</Text>
        </Stack>
      </Group>

      <Tabs value={tab} onChange={(value) => onTab((value as Tab) ?? 'overview')} keepMounted={false}>
        <Tabs.List mb="md">
          <Tabs.Tab value="overview" leftSection={<LayoutDashboard size={16} />}>Overview</Tabs.Tab>
          <Tabs.Tab value="members" leftSection={<Users size={16} />}>Members</Tabs.Tab>
          <Tabs.Tab value="projects" leftSection={<FolderGit2 size={16} />}>Projects</Tabs.Tab>
          {isAdministrator && <Tabs.Tab value="settings" leftSection={<Settings size={16} />}>Settings</Tabs.Tab>}
        </Tabs.List>

        <Tabs.Panel value="overview">
          <Overview
            description={organization.description}
            memberCount={memberCount}
            projectCount={projectCount}
            onTab={onTab}
          />
        </Tabs.Panel>
        <Tabs.Panel value="members">
          <Members organization={organization} isAdministrator={isAdministrator} />
        </Tabs.Panel>
        <Tabs.Panel value="projects">
          <Projects organizationId={organization.id} />
        </Tabs.Panel>
        {isAdministrator && (
          <Tabs.Panel value="settings">
            <OrganizationSettings organization={organization} />
          </Tabs.Panel>
        )}
      </Tabs>
    </Stack>
  );
}

function Overview({
  description,
  memberCount,
  projectCount,
  onTab,
}: {
  description: string;
  memberCount: number | undefined;
  projectCount: number | undefined;
  onTab: (tab: Tab) => void;
}) {
  return (
    <Stack gap="md">
      <Text size="sm" c={description ? undefined : 'dimmed'}>{description || 'No description.'}</Text>
      <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md" maw={640}>
        <Card withBorder radius="lg" p="lg">
          <Text size="sm" c="dimmed">Members</Text>
          <Text fz={28} fw={700}>{memberCount ?? '…'}</Text>
          <Anchor component="button" size="sm" onClick={() => onTab('members')}>See who is in it</Anchor>
        </Card>
        <Card withBorder radius="lg" p="lg">
          <Text size="sm" c="dimmed">Projects</Text>
          <Text fz={28} fw={700}>{projectCount ?? '…'}</Text>
          <Anchor component="button" size="sm" onClick={() => onTab('projects')}>See its projects</Anchor>
        </Card>
      </SimpleGrid>
    </Stack>
  );
}

const MEMBER_COLUMNS = 4;

function Members({
  organization,
  isAdministrator,
}: {
  organization: { id: string; name: string };
  isAdministrator: boolean;
}) {
  const { data, isLoading, error, refetch } = useUsers();
  // Mounted only while open, so each opening starts from the account it was opened for.
  const [dialog, setDialog] = useState<{ account: ApiOrganizationUser | null } | null>(null);
  const users = data?.users ?? [];

  return (
    <Stack gap="md">
      {isAdministrator && (
        <Group justify="flex-end">
          <Button leftSection={<Plus size={16} />} onClick={() => setDialog({ account: null })}>Add member</Button>
        </Group>
      )}
      <Card withBorder radius="lg" p={0}>
        {isLoading ? (
          <TableLoadingState rows={4} columns={MEMBER_COLUMNS} />
        ) : error ? (
          <ErrorState error={error} action="load the members" onRetry={() => refetch()} />
        ) : (
          <Table.ScrollContainer minWidth={700}>
            <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
              <Table.Thead bg="gray.0">
                <Table.Tr>
                  <Table.Th>Member</Table.Th>
                  <Table.Th>Email</Table.Th>
                  <Table.Th>Roles</Table.Th>
                  <Table.Th ta="right">Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {users.length === 0 ? (
                  <Table.Tr>
                    <Table.Td colSpan={MEMBER_COLUMNS}>
                      <EmptyState icon={Users} title="Nobody here yet" description="Add the people who will design and run processes in this organization." />
                    </Table.Td>
                  </Table.Tr>
                ) : (
                  users.map((u) => (
                    <Table.Tr key={u.id}>
                      <Table.Td>
                        <Stack gap={0}>
                          <Text fw={600} size="sm">{u.full_name || u.username}</Text>
                          <Text size="xs" c="dimmed">@{u.username}</Text>
                        </Stack>
                      </Table.Td>
                      <Table.Td><Text size="sm">{u.email || '—'}</Text></Table.Td>
                      <Table.Td><AccountRoleBadges account={u} /></Table.Td>
                      <Table.Td>
                        {isAdministrator && (
                          <Group justify="flex-end">
                            <Tooltip label="Change roles">
                              <ActionIcon
                                aria-label={`Change roles for ${u.full_name || u.username}`}
                                variant="light"
                                color="indigo"
                                onClick={() => setDialog({ account: u })}
                              >
                                <Edit2 size={16} />
                              </ActionIcon>
                            </Tooltip>
                          </Group>
                        )}
                      </Table.Td>
                    </Table.Tr>
                  ))
                )}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Card>
      <Text size="xs" c="dimmed" maw={720}>
        Roles shown with this organization's name count here only. Roles held in every organization are changed by a
        platform administrator on Platform access.
      </Text>
      {dialog && (
        <AccountDialog opened onClose={() => setDialog(null)} account={dialog.account} organization={organization} />
      )}
    </Stack>
  );
}

function Projects({ organizationId }: { organizationId: string }) {
  const { data, isLoading, error, refetch } = useProjects(organizationId);
  const projects = data?.projects ?? [];

  if (isLoading) return <TableLoadingState rows={3} columns={2} />;
  if (error) return <ErrorState error={error} action="load the projects" onRetry={() => refetch()} />;

  return (
    <Stack gap="md">
      <Group justify="flex-end">
        <Button component={Link} to="/projects" variant="default">Manage projects</Button>
      </Group>
      <Card withBorder radius="lg" p={0}>
        {projects.length === 0 ? (
          <EmptyState icon={FolderGit2} title="No projects yet" description="A project holds process models, decisions and the connections they use." />
        ) : (
          <Table verticalSpacing="md" horizontalSpacing="xl">
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Project</Table.Th>
                <Table.Th>Description</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {projects.map((p) => (
                <Table.Tr key={p.id}>
                  <Table.Td><Text fw={600} size="sm">{p.name}</Text></Table.Td>
                  <Table.Td><Text size="sm" lineClamp={1}>{p.description || '—'}</Text></Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Card>
    </Stack>
  );
}

function OrganizationSettings({ organization }: { organization: { id: string; name: string; description: string } }) {
  const [name, setName] = useState(organization.name);
  const [description, setDescription] = useState(organization.description);
  const updateOrg = useUpdateOrganization();
  const deleteOrg = useDeleteOrganization();
  const switchOrganization = useSwitchOrganization();
  const navigate = useNavigate();
  const changed = name.trim() !== organization.name || description !== organization.description;

  const save = async () => {
    try {
      await updateOrg.mutateAsync({ id: organization.id, name: name.trim(), description });
      notifications.show({ title: 'Saved', message: `${name.trim()} was updated.`, color: 'green' });
    } catch (error) {
      notifications.show({ title: 'Could not save it', message: failureMessage('Failed to save organization', error), color: 'red' });
    }
  };

  const remove = async () => {
    // The server refuses while anybody else is in it or any project is; its
    // refusal says what is still there, and is shown as it is.
    if (!window.confirm(`Delete ${organization.name}? This cannot be undone.`)) return;
    try {
      await deleteOrg.mutateAsync(organization.id);
      notifications.show({ title: 'Deleted', message: `${organization.name} is gone.`, color: 'green' });
      // Its id is no longer one of the caller's; the layout picks another.
      switchOrganization(null);
      void navigate({ to: '/organizations' });
    } catch (error) {
      notifications.show({ title: 'Could not delete it', message: failureMessage('Failed to delete organization', error), color: 'red' });
    }
  };

  return (
    <Stack gap="xl" maw={560}>
      <Stack gap="md">
        <TextInput label="Name" required value={name} onChange={(e) => setName(e.currentTarget.value)} />
        <Textarea label="Description" minRows={3} value={description} onChange={(e) => setDescription(e.currentTarget.value)} />
        <Group>
          <Button onClick={save} loading={updateOrg.isPending} disabled={!changed || !name.trim()}>Save</Button>
        </Group>
      </Stack>
      <Card withBorder radius="lg" p="lg" style={{ borderColor: 'var(--mantine-color-red-3)' }}>
        <Stack gap="xs">
          <Text fw={600}>Delete this organization</Text>
          <Text size="sm" c="dimmed">
            Possible only once nobody else is in it and it has no projects.
          </Text>
          <Group>
            <Button color="red" variant="light" onClick={remove} loading={deleteOrg.isPending}>Delete organization</Button>
          </Group>
        </Stack>
      </Card>
    </Stack>
  );
}
