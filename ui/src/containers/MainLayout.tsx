import { AppShell, Box, Button, Divider, Drawer, Group, Paper, Stack, Text, ThemeIcon, Timeline, Title } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { Link, useLocation } from '@tanstack/react-router';
import { BookOpen, ExternalLink, FolderGit2, Lightbulb } from 'lucide-react';
import React from 'react';
import { AppHeader, Sidebar } from '../components/shell';
import { EmptyState } from '../components/state';
import { useOrganizations } from '../hooks/useOrganization';
import { useProjects } from '../hooks/useProcess';
import { useAppStore } from '../store/useAppStore';

interface MainLayoutProps {
  children: React.ReactNode;
}

const NAV_WIDTH_EXPANDED = 240;
const NAV_WIDTH_COLLAPSED = 68;

/**
 * The application shell.
 *
 * Every surface here previously branched on `theme === 'dark' ? … : …` inline —
 * the header background, its border, the main background. That was repeated per
 * element and guaranteed to drift the moment someone added a surface and forgot
 * the dark branch. Colour now resolves in CSS through light-dark(), which works
 * since postcss-preset-mantine was installed, so there is one definition per
 * surface and dark mode cannot be half-applied.
 */
export function MainLayout({ children }: MainLayoutProps) {
  const [navOpened, { toggle: toggleNav }] = useDisclosure();
  const [helpOpened, { open: openHelp, close: closeHelp }] = useDisclosure(false);

  const {
    currentProjectId,
    sidebarExpanded,
    currentOrganizationId,
    setCurrentOrganizationId,
    setCurrentProjectId,
    user,
  } = useAppStore();
  const { data: organizationsData } = useOrganizations();
  const { data: projectsData } = useProjects(currentOrganizationId);
  const location = useLocation();

  const organizations = organizationsData?.organizations ?? user?.organizations ?? [];
  const projects = projectsData?.projects ?? [];

  // Fall back to the first organization the caller belongs to when the stored
  // one is no longer among them.
  React.useEffect(() => {
    if (organizations.length === 0) return;
    const stillAMember = organizations.some((o: { id: string }) => o.id === currentOrganizationId);
    if (!stillAMember) {
      setCurrentOrganizationId(organizations[0].id);
      setCurrentProjectId(null);
    }
  }, [organizations, currentOrganizationId, setCurrentOrganizationId, setCurrentProjectId]);

  const isDesigner = location.pathname.includes('/designer');
  const worksWithoutProject =
    location.pathname === '/' ||
    ['/projects', '/organizations', '/users', '/groups', '/settings', '/profile'].some((path) =>
      location.pathname.includes(path),
    );

  return (
    <AppShell
      header={{ height: 60 }}
      navbar={{
        width: sidebarExpanded ? NAV_WIDTH_EXPANDED : NAV_WIDTH_COLLAPSED,
        breakpoint: 'sm',
        collapsed: { mobile: !navOpened },
      }}
      padding={0}
      transitionDuration={160}
    >
      <AppShell.Header withBorder={false}>
        <AppHeader
          navOpened={navOpened}
          onNavToggle={toggleNav}
          onHelpOpen={openHelp}
          organizations={organizations}
          projects={projects}
        />
      </AppShell.Header>

      <AppShell.Navbar withBorder={false}>
        <Sidebar />
      </AppShell.Navbar>

      <AppShell.Main
        style={{
          backgroundColor: 'light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-8))',
          minHeight: '100vh',
        }}
      >
        {/*
          A max width on text-and-table content. Without one, a table stretches
          to whatever the monitor is, and the eye loses the row it was reading
          on the way across. The designer is exempt: a canvas wants every pixel.
        */}
        <Box p={isDesigner ? 0 : 'xl'} maw={isDesigner ? undefined : 1440} mx="auto">
          {!currentProjectId && !worksWithoutProject ? (
            <EmptyState
              icon={FolderGit2}
              title="Choose a project to continue"
              description="Processes, tasks and instances all belong to a project. Pick one from the header, or create your first."
              action={
                <Button component={Link} to="/projects">
                  Go to projects
                </Button>
              }
            />
          ) : (
            children
          )}
        </Box>
      </AppShell.Main>

      <Drawer opened={helpOpened} onClose={closeHelp} position="right" size="md" title={<Text fw={600}>Help</Text>}>
        <Stack gap="xl">
          <Paper p="md" radius="md" bg="var(--mantine-color-blue-light)">
            <Group align="flex-start" wrap="nowrap" gap="sm">
              <ThemeIcon variant="light" color="blue" size="sm">
                <Lightbulb size={14} />
              </ThemeIcon>
              <Text size="sm">
                In the process designer, press <b>Cmd + K</b> (or Ctrl + K) to search nodes and actions.
              </Text>
            </Group>
          </Paper>

          <Box>
            <Title order={5} mb="md">Getting started</Title>
            <Timeline active={-1} bulletSize={22} lineWidth={2}>
              <Timeline.Item title="Create a project">
                <Text c="dimmed" size="xs">Projects group related processes, decisions and tasks.</Text>
              </Timeline.Item>
              <Timeline.Item title="Design a process">
                <Text c="dimmed" size="xs">Model the flow of work with the drag-and-drop designer.</Text>
              </Timeline.Item>
              <Timeline.Item title="Connect other systems">
                <Text c="dimmed" size="xs">Call an API, send a message, or hand work to an external worker.</Text>
              </Timeline.Item>
              <Timeline.Item title="Deploy and watch it run">
                <Text c="dimmed" size="xs">Start instances and follow them from the Instances view.</Text>
              </Timeline.Item>
            </Timeline>
          </Box>

          <Divider label="Reference" labelPosition="center" />

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
