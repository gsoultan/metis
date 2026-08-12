import {
  ActionIcon,
  Avatar,
  Box,
  Burger,
  Group,
  Menu,
  Select,
  Switch,
  Text,
  Tooltip,
  UnstyledButton,
} from '@mantine/core';
import { Link } from '@tanstack/react-router';
import {
  Building2,
  FolderGit2,
  HelpCircle,
  LogOut,
  Moon,
  Settings,
  Sun,
  User,
} from 'lucide-react';
import { useAppStore } from '../../store/useAppStore';
import { NotificationCenter } from '../NotificationCenter';
import classes from './AppHeader.module.css';

interface AppHeaderProps {
  navOpened: boolean;
  onNavToggle: () => void;
  onHelpOpen: () => void;
  organizations: Array<{ id: string; name: string }>;
  projects: Array<{ id: string; name: string }>;
}

/**
 * Application header: where you are, and who you are.
 *
 * The previous header packed two 200px selects, a redundant project chip,
 * notifications, help and a user menu into a single unbreaking row. At 1280px
 * with both selects populated it was already tight, and below that it
 * overflowed — there was not one responsive prop on any of it.
 *
 * Three changes:
 *
 *  - **The context selectors collapse before anything else.** On small screens
 *    they move into a single menu, because knowing *which project* matters more
 *    than being able to change it from every viewport.
 *  - **The project chip is gone.** It repeated the name already shown in the
 *    select immediately beside it.
 *  - **Theme moved here** from the navigation rail, next to the other personal
 *    settings, where people look for it.
 */
export function AppHeader({
  navOpened,
  onNavToggle,
  onHelpOpen,
  organizations,
  projects,
}: AppHeaderProps) {
  const {
    theme,
    toggleTheme,
    user,
    clearAuth,
    currentProjectId,
    setCurrentProjectId,
    currentOrganizationId,
    setCurrentOrganizationId,
    expertMode,
    setExpertMode,
  } = useAppStore();

  const isDark = theme === 'dark';
  const currentProject = projects.find((p) => p.id === currentProjectId);

  const contextSelectors = (
    <>
      {organizations.length > 1 && (
        <Select
          aria-label="Organization"
          placeholder="Organization"
          leftSection={<Building2 size={15} />}
          data={organizations.map((o) => ({ value: o.id, label: o.name }))}
          value={currentOrganizationId}
          onChange={(value) => {
            setCurrentOrganizationId(value);
            // The previous project belongs to the previous organization.
            setCurrentProjectId(null);
          }}
          size="sm"
          w={190}
          comboboxProps={{ withinPortal: true }}
          allowDeselect={false}
        />
      )}

      <Select
        aria-label="Project"
        placeholder="Select a project"
        leftSection={<FolderGit2 size={15} />}
        data={projects.map((p) => ({ value: p.id, label: p.name }))}
        value={currentProjectId}
        onChange={setCurrentProjectId}
        size="sm"
        w={200}
        comboboxProps={{ withinPortal: true }}
        allowDeselect={false}
        nothingFoundMessage="No projects in this organization"
      />
    </>
  );

  return (
    <header className={classes.header}>
      <Group h="100%" px="md" justify="space-between" wrap="nowrap" gap="sm">
        <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
          <Burger opened={navOpened} onClick={onNavToggle} hiddenFrom="sm" size="sm" aria-label="Toggle navigation" />

          {/* Context selectors: full controls on desktop… */}
          <Group gap="xs" wrap="nowrap" visibleFrom="md">
            {contextSelectors}
          </Group>

          {/* …and the current location, tappable, below that. */}
          <Box hiddenFrom="md" style={{ minWidth: 0 }}>
            <Menu position="bottom-start" width={260} withinPortal>
              <Menu.Target>
                <UnstyledButton className={classes.contextButton}>
                  <FolderGit2 size={15} />
                  <Text size="sm" fw={500} truncate>
                    {currentProject?.name ?? 'Select a project'}
                  </Text>
                </UnstyledButton>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>Switch context</Menu.Label>
                <Box p="xs">
                  <Group gap="xs" wrap="wrap">
                    {contextSelectors}
                  </Group>
                </Box>
              </Menu.Dropdown>
            </Menu>
          </Box>
        </Group>

        <Group gap={4} wrap="nowrap">
          <NotificationCenter />

          <Tooltip label={isDark ? 'Switch to light theme' : 'Switch to dark theme'} withArrow>
            <ActionIcon
              variant="subtle"
              color="gray"
              size="lg"
              onClick={toggleTheme}
              aria-label={isDark ? 'Switch to light theme' : 'Switch to dark theme'}
            >
              {isDark ? <Sun size={18} /> : <Moon size={18} />}
            </ActionIcon>
          </Tooltip>

          <Tooltip label="Help" withArrow>
            <ActionIcon variant="subtle" color="gray" size="lg" onClick={onHelpOpen} aria-label="Open help">
              <HelpCircle size={18} />
            </ActionIcon>
          </Tooltip>

          <Menu shadow="md" width={230} position="bottom-end" withinPortal>
            <Menu.Target>
              <UnstyledButton className={classes.userButton} aria-label="Account menu">
                <Avatar color="blue" radius="xl" size={30}>
                  {(user?.displayName || user?.name || 'U').charAt(0).toUpperCase()}
                </Avatar>
                <Box visibleFrom="sm" style={{ minWidth: 0, textAlign: 'left' }}>
                  <Text size="sm" fw={500} truncate>
                    {user?.displayName || user?.name}
                  </Text>
                  {/*
                    This rendered the raw role value — "ADMIN" — at every login.
                    The username is what a person recognises as themselves.
                  */}
                  <Text size="xs" c="dimmed" truncate>
                    {user?.username}
                  </Text>
                </Box>
              </UnstyledButton>
            </Menu.Target>

            <Menu.Dropdown>
              <Menu.Item component={Link} to="/profile" leftSection={<User size={14} />}>
                Profile
              </Menu.Item>
              <Menu.Item component={Link} to="/settings" leftSection={<Settings size={14} />}>
                Settings
              </Menu.Item>

              <Menu.Divider />

              <Menu.Item
                closeMenuOnClick={false}
                onClick={() => setExpertMode(!expertMode)}
                rightSection={
                  <Switch
                    checked={expertMode}
                    onChange={(event) => setExpertMode(event.currentTarget.checked)}
                    size="xs"
                    aria-label="Expert mode"
                    tabIndex={-1}
                  />
                }
              >
                <Text size="sm">Expert mode</Text>
                <Text size="xs" c="dimmed">
                  Show advanced technical settings
                </Text>
              </Menu.Item>

              <Menu.Divider />

              <Menu.Item color="red" leftSection={<LogOut size={14} />} onClick={clearAuth}>
                Sign out
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Group>
    </header>
  );
}
