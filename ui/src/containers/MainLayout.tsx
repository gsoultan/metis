import { useMemo } from 'react';
import { AppShell, Box, Button } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { Link, useLocation } from '@tanstack/react-router';
import { FolderGit2 } from 'lucide-react';
import React from 'react';
import { HelpDrawer } from '../components/HelpDrawer';
import { AppHeader, Sidebar } from '../components/shell';
import { EmptyState } from '../components/state';
import { selectionNeedsUpdate, resolveSelection } from '../domain/activeSelection';
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

  // A fresh [] each render would restart the effect below on every render.
  const organizations = useMemo(
    () => organizationsData?.organizations ?? user?.organizations ?? [],
    [organizationsData, user],
  );
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData]);

  /*
   * Nobody should have to choose a project before the app will show them
   * anything.
   *
   * Signing in left both of these unset, so the first thing after entering a
   * password was "Choose a project to continue" on every screen — even for
   * somebody who belongs to exactly one. The rules live in
   * domain/activeSelection.ts: keep what is selected if it is still available,
   * otherwise take the first, and only answer "none" when there really is
   * nothing.
   *
   * Both effects wait for the list to have *loaded*. An empty array means
   * "there are none", so acting on one that is merely in flight would clear a
   * good selection and flash the empty state on every reload.
   */
  const organizationsLoaded = organizations.length > 0;
  React.useEffect(() => {
    if (!organizationsLoaded) return;
    if (!selectionNeedsUpdate(organizations, currentOrganizationId)) return;
    setCurrentOrganizationId(resolveSelection(organizations, currentOrganizationId));
    // The projects belong to the old organization, so the stored one cannot be
    // right; the effect below picks the new organization's first.
    setCurrentProjectId(null);
  }, [organizationsLoaded, organizations, currentOrganizationId, setCurrentOrganizationId, setCurrentProjectId]);

  const projectsLoaded = projectsData !== undefined;
  React.useEffect(() => {
    if (!projectsLoaded) return;
    if (!selectionNeedsUpdate(projects, currentProjectId)) return;
    setCurrentProjectId(resolveSelection(projects, currentProjectId));
  }, [projectsLoaded, projects, currentProjectId, setCurrentProjectId]);

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

      <AppShell.Navbar withBorder={false} aria-label="Main navigation">
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
          {!currentProjectId && !worksWithoutProject && projectsLoaded ? (
            /*
              A project is chosen automatically, so reaching this means there
              is none to choose — a brand new account, or one whose access was
              withdrawn. It used to say "pick one from the header" even when the
              header had nothing in it to pick.

              Gated on projectsLoaded so a reload does not flash this while the
              list is still on its way.
            */
            <EmptyState
              icon={FolderGit2}
              title="You are not in a project yet"
              description="Processes, tasks and instances all belong to a project. Create one to get started, or ask an administrator to add you to theirs."
              action={
                <Button component={Link} to="/projects">
                  Go to projects
                </Button>
              }
            />
          ) : !currentProjectId && !worksWithoutProject ? null : (
            children
          )}
        </Box>
      </AppShell.Main>

      <HelpDrawer opened={helpOpened} onClose={closeHelp} />
    </AppShell>
  );
}
