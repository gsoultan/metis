import { Button, Divider, Drawer, Group, Paper, Stack, Tabs, Text, ThemeIcon } from '@mantine/core';
import { BookOpen, ExternalLink, Lightbulb } from 'lucide-react';

import { gettingStartedFacts } from '../domain/gettingStarted';
import { useParticipants } from '../hooks/useParticipants';
import { useConnectorInstances, useDefinitions, useInstances, useProcessStatistics } from '../hooks/useProcess';
import { GettingStartedTimeline } from './GettingStartedCard';
import { GlossaryPanel } from './GlossaryPanel';

/**
 * Help: where to start, and what the words mean.
 *
 * Two tabs rather than one long page, because they are read for different
 * reasons: the checklist while somebody is new, the glossary whenever a word
 * on some screen stops them.
 */
export function HelpDrawer({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  return (
    <Drawer opened={opened} onClose={onClose} position="right" size="md" title={<Text fw={600}>Help</Text>}>
      <HelpTabs onNavigate={onClose} />
    </Drawer>
  );
}

/**
 * What the drawer holds. Apart from the drawer, whose content is drawn only
 * while it is open, so it can be drawn on its own.
 */
export function HelpTabs({ onNavigate }: { onNavigate: () => void }) {
  return (
    <Tabs defaultValue="getting-started">
      <Tabs.List grow mb="lg">
        <Tabs.Tab value="getting-started">Getting started</Tabs.Tab>
        <Tabs.Tab value="glossary">Glossary</Tabs.Tab>
      </Tabs.List>
      <Tabs.Panel value="getting-started">
        <Stack gap="xl">
          <GettingStartedProgress onNavigate={onNavigate} />
          <DesignerSearchTip />
          <Divider label="Reference" labelPosition="center" />
          <ReferenceLinks />
        </Stack>
      </Tabs.Panel>
      <Tabs.Panel value="glossary">
        <GlossaryPanel />
      </Tabs.Panel>
    </Tabs>
  );
}

function DesignerSearchTip() {
  return (
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
  );
}

const REFERENCE_LINKS = [
  { label: 'BPMN 2.0 specification', href: 'https://www.omg.org/spec/BPMN/2.0/' },
  { label: 'Project repository', href: 'https://github.com/gsoultan/metis' },
];

function ReferenceLinks() {
  return (
    <Stack gap="xs">
      {REFERENCE_LINKS.map((link) => (
        <Button
          key={link.href}
          variant="light"
          component="a"
          href={link.href}
          target="_blank"
          rel="noreferrer noopener"
          leftSection={<BookOpen size={16} />}
          rightSection={<ExternalLink size={14} />}
          justify="flex-start"
        >
          {link.label}
        </Button>
      ))}
    </Stack>
  );
}

/**
 * Getting started, ticked from what this project has actually done.
 *
 * It lives inside the drawer, which renders its content only while open, so
 * these requests are made when somebody opens Help and not on every page. The
 * hooks and arguments are the ones the dashboard uses, so a list it has already
 * loaded comes from the cache rather than being asked for again.
 */
function GettingStartedProgress({ onNavigate }: { onNavigate: () => void }) {
  const { data: definitions } = useDefinitions();
  // Not live: this only asks whether any instance exists, and a live list
  // polls every few seconds.
  const { data: instances } = useInstances(1, 25, {}, false);
  // Counted across the project. The newest 200 tasks missed a completed one
  // behind 200 open ones.
  const { data: statistics } = useProcessStatistics();
  const { data: connections } = useConnectorInstances();
  const { data: people } = useParticipants();
  const facts = gettingStartedFacts({
    definitions: definitions?.definitions,
    instances: instances?.instances,
    completedTasks: statistics?.stats?.completedTasks,
    connections: connections?.instances,
    people: people?.participants,
  });

  return <GettingStartedTimeline facts={facts} onNavigate={onNavigate} />;
}
