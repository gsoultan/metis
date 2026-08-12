import { AppShell, Burger, Box, Select, Group, Text, Paper, ThemeIcon, Stack, Button, Menu, Avatar, UnstyledButton, Switch, Drawer, Timeline, Title, ActionIcon, Divider } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { Sidebar } from '../components/Sidebar';
import React from 'react';
import { useAppStore } from '../store/useAppStore';
import { useProjects } from '../hooks/useProcess';
import { useOrganizations } from '../hooks/useOrganization';
import { FolderGit2, AlertCircle, LogOut, User, Settings, ShieldCheck, ShieldOff, HelpCircle, BookOpen, Lightbulb, ExternalLink } from 'lucide-react';
import { Link, useLocation } from '@tanstack/react-router';
import { NotificationCenter } from '../components/NotificationCenter';

/** An organization or project as listed in the switcher. */
type NamedRef = { id: string; name: string };

interface MainLayoutProps {
  children: React.ReactNode;
  activeTab?: string;
  onTabChange?: (tab: string) => void;
}

export function MainLayout({ children }: MainLayoutProps) {
  const [opened, { toggle }] = useDisclosure();
  const [helpOpened, { open: openHelp, close: closeHelp }] = useDisclosure(false);
  const { theme, currentProjectId, setCurrentProjectId, sidebarExpanded, user, clearAuth, currentOrganizationId, setCurrentOrganizationId, expertMode, setExpertMode } = useAppStore();
  const { data: organizationsData } = useOrganizations();
  const { data: projectsData } = useProjects(currentOrganizationId);
  const location = useLocation();
  const organizations = organizationsData?.organizations || user?.organizations || [];

  // Sync currentOrganizationId from available organizations
  React.useEffect(() => {
    if (organizations.length === 0) {
      return;
    }

    const hasCurrentOrganization = organizations.some((organization: NamedRef) => organization.id === currentOrganizationId);
    if (!hasCurrentOrganization) {
      setCurrentOrganizationId(organizations[0].id);
      setCurrentProjectId(null);
    }
  }, [organizations, currentOrganizationId, setCurrentOrganizationId, setCurrentProjectId]);

  const isDesigner = location.pathname.includes('/designer');
  const isProjectsPage = location.pathname.includes('/projects');
  const isDashboard = location.pathname === '/';

  const projects = projectsData?.projects || [];
  const currentProject = projects.find((p: NamedRef) => p.id === currentProjectId);

  return (
    <AppShell
      header={{ height: 60 }}
      navbar={{
        width: sidebarExpanded ? 240 : 80,
        breakpoint: 'sm',
        collapsed: { mobile: !opened },
      }}
      padding="0"
      transitionDuration={200}
    >
      <AppShell.Header bg={theme === 'dark' ? 'dark.7' : 'white'} style={{ borderBottom: `1px solid ${theme === 'dark' ? 'var(--mantine-color-dark-4)' : 'var(--mantine-color-gray-2)'}` }}>
        <Group h="100%" px="md" justify="space-between">
          <Group>
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <Text fw={800} size="xl" variant="gradient" gradient={{ from: 'blue', to: 'cyan' }}>Hermod BPM</Text>
          </Group>
          
          <Group gap="lg">
            <Group>
              {organizations.length > 0 && (
                <Select
                  placeholder="Select Organization"
                  data={organizations.map((organization: NamedRef) => ({ value: organization.id, label: organization.name }))}
                  value={currentOrganizationId}
                  onChange={(val) => {
                    setCurrentOrganizationId(val);
                    setCurrentProjectId(null); // Clear project when organization changes
                  }}
                  style={{ width: 200 }}
                  variant="filled"
                  radius="md"
                />
              )}
              {projects.length > 0 ? (
                <Select
                  placeholder="Select Project"
                  data={projects.map((p: NamedRef) => ({ value: p.id, label: p.name }))}
                  value={currentProjectId}
                  onChange={setCurrentProjectId}
                  leftSection={<FolderGit2 size={16} />}
                  style={{ width: 200 }}
                  variant="filled"
                  radius="md"
                />
              ) : (
                <Button
                  component={Link}
                  to="/projects"
                  variant="light"
                  size="xs"
                  leftSection={<FolderGit2 size={14} />}
                >
                  Create your first project
                </Button>
              )}
              
              {currentProject && (
                <Paper withBorder px="xs" py={4} radius="md" bg={theme === 'dark' ? 'dark.6' : 'gray.0'}>
                  <Group gap="xs">
                    <ThemeIcon size="xs" variant="light" color="green" radius="xl">
                      <FolderGit2 size={10} />
                    </ThemeIcon>
                    <Text size="xs" fw={700}>{currentProject.name}</Text>
                  </Group>
                </Paper>
              )}
            </Group>

            <NotificationCenter />

            <ActionIcon variant="subtle" color="gray" size="lg" radius="xl" onClick={openHelp}>
              <HelpCircle size={20} />
            </ActionIcon>

            <Menu shadow="md" width={200} position="bottom-end">
              <Menu.Target>
                <UnstyledButton>
                  <Group gap="xs">
                    <Avatar color="blue" radius="xl" size="sm">
                      {user?.name?.charAt(0) || 'U'}
                    </Avatar>
                    <Box visibleFrom="sm">
                      <Text size="sm" fw={600}>{user?.name}</Text>
                      <Text size="xs" c="dimmed">{user?.role}</Text>
                    </Box>
                  </Group>
                </UnstyledButton>
              </Menu.Target>

              <Menu.Dropdown>
                <Menu.Label>Application</Menu.Label>
                <Menu.Item component={Link} to="/profile" leftSection={<User size={14} />}>Profile</Menu.Item>
                <Menu.Item component={Link} to="/settings" leftSection={<Settings size={14} />}>Settings</Menu.Item>
                <Menu.Item 
                  closeMenuOnClick={false}
                  leftSection={expertMode ? <ShieldCheck size={14} color="green" /> : <ShieldOff size={14} color="gray" />}
                  rightSection={
                    <Switch 
                      checked={expertMode} 
                      onChange={(event) => setExpertMode(event.currentTarget.checked)} 
                      size="xs" 
                    />
                  }
                >
                  Expert Mode
                </Menu.Item>
                <Menu.Divider />
                <Menu.Label>Danger zone</Menu.Label>
                <Menu.Item 
                  color="red" 
                  leftSection={<LogOut size={14} />}
                  onClick={clearAuth}
                >
                  Logout
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar>
        <Sidebar />
      </AppShell.Navbar>

      <AppShell.Main bg={theme === 'dark' ? 'dark.8' : 'gray.0'} style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
        <Box p={isDesigner ? 0 : "xl"} style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          {!currentProjectId && !isProjectsPage && !isDashboard ? (
            <Stack align="center" py={100} gap="md">
              <ThemeIcon size={80} radius="xl" variant="light" color="orange">
                <AlertCircle size={40} />
              </ThemeIcon>
              <Text fw={800} size="xl">No Project Selected</Text>
              <Text c="dimmed" ta="center" maw={500}>
                Please select a project from the header or go to the Projects page to create/select one before managing definitions or tasks.
              </Text>
              <Button component={Link} to="/projects" variant="filled" color="indigo">
                Go to Projects
              </Button>
            </Stack>
          ) : (
            children
          )}
        </Box>
      </AppShell.Main>

      <Drawer 
        opened={helpOpened} 
        onClose={closeHelp} 
        title={<Group gap="xs"><HelpCircle size={20} color="var(--mantine-color-blue-6)" /><Text fw={800}>Hermod Help Center</Text></Group>} 
        position="right"
        size="md"
      >
        <Stack gap="xl">
          <Paper withBorder p="md" radius="md" bg="blue.0">
            <Group align="flex-start" wrap="nowrap">
              <ThemeIcon variant="light" color="blue"><Lightbulb size={16} /></ThemeIcon>
              <Box>
                <Text fw={700} size="sm">Quick Tip</Text>
                {/* This claimed Cmd+K worked "anywhere". It is wired only in
                    the process designer, so the tip was wrong everywhere else
                    the drawer can be opened. */}
                <Text size="xs">In the process designer, press <b>Cmd + K</b> (or Ctrl + K) to search nodes and actions.</Text>
              </Box>
            </Group>
          </Paper>

          <Box>
            <Title order={5} mb="md">Getting Started Guide</Title>
            <Timeline active={1} bulletSize={24} lineWidth={2}>
              <Timeline.Item bullet={<Text size="xs" fw={700}>1</Text>} title="Create a Project">
                <Text c="dimmed" size="xs">Organize your process models into projects.</Text>
              </Timeline.Item>
              <Timeline.Item bullet={<Text size="xs" fw={700}>2</Text>} title="Design your Process">
                <Text c="dimmed" size="xs">Use the drag-and-drop designer to model your workflow.</Text>
              </Timeline.Item>
              <Timeline.Item bullet={<Text size="xs" fw={700}>3</Text>} title="Configure Connectors">
                <Text c="dimmed" size="xs">Integrate with Slack, Email, or HTTP services.</Text>
              </Timeline.Item>
              <Timeline.Item bullet={<Text size="xs" fw={700}>4</Text>} title="Deploy & Run">
                <Text c="dimmed" size="xs">Start instances and monitor their execution.</Text>
              </Timeline.Item>
            </Timeline>
          </Box>

          <Divider label="Documentation" labelPosition="center" />
          
          {/*
            These four buttons navigated nowhere, and the support hours
            ("Mon-Fri, 9am-5pm") described a team that does not exist. Help is
            the last place a product can afford to be wrong: someone opens it
            because they are already stuck, so failing them here turns
            confusion into distrust. Real links, or nothing.
          */}
          <Stack gap="xs">
            <Button
              variant="light"
              component="a"
              href="https://www.omg.org/spec/BPMN/2.0/"
              target="_blank"
              rel="noreferrer noopener"
              leftSection={<BookOpen size={16} />}
              rightSection={<ExternalLink size={14} />}
              justify="flex-start"
            >
              BPMN 2.0 specification
            </Button>
            <Button
              variant="light"
              component="a"
              href="https://github.com/gsoultan/gobpm"
              target="_blank"
              rel="noreferrer noopener"
              leftSection={<BookOpen size={16} />}
              rightSection={<ExternalLink size={14} />}
              justify="flex-start"
            >
              Project repository
            </Button>
          </Stack>
        </Stack>
      </Drawer>
    </AppShell>
  );
}
