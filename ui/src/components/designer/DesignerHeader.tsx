import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Group,
  Menu,
  SegmentedControl,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { ArrowLeft, Download, FileUp, MoreHorizontal, Network, Rocket } from 'lucide-react';

import type { ValidationIssue } from '../../domain/processValidation';
import { IssuesButton } from './IssuesButton';
import classes from './DesignerHeader.module.css';

interface DesignerHeaderProps {
  processName: string;
  onNameChange: (name: string) => void;
  processKey: string;
  lastSaved: Date | null;
  simulating: boolean;
  onModeChange: (mode: 'design' | 'simulate') => void;
  issues: ValidationIssue[];
  onSelectIssue: (id: string) => void;
  onBack: () => void;
  onImport: () => void;
  onExport: () => void;
  importing: boolean;
  exporting: boolean;
  onDeploy: () => void;
  deploying: boolean;
  /** Why Deploy is unavailable, or null when it is. */
  deployBlockedBy: string | null;
}

/**
 * The designer's one bar: what this process is, and what can be done to it.
 *
 * The name used to be edited in a box floating over the canvas while the same
 * name sat, read-only, as the page title above it — two places for one thing,
 * and the box covered the diagram. It is edited where it is read now. Import
 * and Export, which are done once in a while, moved into a menu so the bar
 * leads with the things done every time: switch mode, check, deploy.
 */
export function DesignerHeader({
  processName,
  onNameChange,
  processKey,
  lastSaved,
  simulating,
  onModeChange,
  issues,
  onSelectIssue,
  onBack,
  onImport,
  onExport,
  importing,
  exporting,
  onDeploy,
  deploying,
  deployBlockedBy,
}: DesignerHeaderProps) {
  return (
    <Group justify="space-between" wrap="nowrap" gap="md" px="md" py={10} className={classes.bar}>
      <Group gap="sm" wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
        <Tooltip label="Back to processes">
          <ActionIcon variant="subtle" color="gray" size="lg" aria-label="Back to processes" onClick={onBack}>
            <ArrowLeft size={18} />
          </ActionIcon>
        </Tooltip>
        <ThemeIcon variant="light" color="indigo" radius="md" size="lg" visibleFrom="sm">
          <Network size={18} />
        </ThemeIcon>
        <Box style={{ flex: 1, minWidth: 0, maxWidth: 520 }}>
          <TextInput
            variant="unstyled"
            aria-label="Process name"
            placeholder="Name this process"
            value={processName}
            onChange={(event) => onNameChange(event.currentTarget.value)}
            classNames={{ input: classes.name }}
          />
          <Group gap={8} wrap="nowrap">
            <Tooltip label="What other processes and the API call it by">
              <Badge
                variant="light"
                color="gray"
                size="sm"
                radius="sm"
                styles={{ label: { textTransform: 'none', fontWeight: 500 } }}
              >
                {processKey}
              </Badge>
            </Tooltip>
            {/*
              "Last saved" used to sit next to an "Autosaving…" badge that
              could never paint, and both described a localStorage write the
              app then never read. It says plainly where the draft is and that
              deploying is what publishes it.
            */}
            <Text size="xs" c="dimmed" truncate>
              {lastSaved
                ? `Draft kept in this browser at ${lastSaved.toLocaleTimeString()} · Deploy to publish`
                : 'Not deployed yet'}
            </Text>
          </Group>
        </Box>
      </Group>

      <Group gap="sm" wrap="nowrap">
        {/*
          Two halves of one screen rather than two screens: you simulate the
          thing you are editing, on the diagram you are looking at. In the URL
          so the mode is linkable.
        */}
        <SegmentedControl
          size="xs"
          radius="md"
          value={simulating ? 'simulate' : 'design'}
          onChange={(value) => onModeChange(value as 'design' | 'simulate')}
          data={[
            { value: 'design', label: 'Design' },
            { value: 'simulate', label: 'Simulate' },
          ]}
          aria-label="Design or simulate this process"
        />

        {!simulating && <IssuesButton issues={issues} onSelect={onSelectIssue} />}

        <Menu position="bottom-end" shadow="md" width={220}>
          <Menu.Target>
            <Tooltip label="More">
              <ActionIcon variant="default" size={30} aria-label="More actions" loading={importing || exporting}>
                <MoreHorizontal size={16} />
              </ActionIcon>
            </Tooltip>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>BPMN file</Menu.Label>
            <Menu.Item leftSection={<FileUp size={14} />} onClick={onImport} rightSection={<Text size="xs" c="dimmed">Ctrl I</Text>}>
              Import…
            </Menu.Item>
            <Menu.Item leftSection={<Download size={14} />} onClick={onExport} rightSection={<Text size="xs" c="dimmed">Ctrl E</Text>}>
              Export
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>

        {/*
          Was the filled primary button on an empty, invalid process — the
          most prominent control on screen offering the one action that could
          not succeed. It is disabled until the process has something to deploy
          and no blocking errors, and says why.
        */}
        <Tooltip label={deployBlockedBy ?? 'Deploy this version so new instances can start on it'}>
          <Button
            variant="filled"
            color="indigo"
            size="xs"
            leftSection={<Rocket size={14} />}
            onClick={onDeploy}
            loading={deploying}
            disabled={deployBlockedBy !== null}
            data-disabled={deployBlockedBy !== null ? true : undefined}
          >
            Deploy
          </Button>
        </Tooltip>
      </Group>
    </Group>
  );
}
