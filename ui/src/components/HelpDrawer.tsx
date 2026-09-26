import { Button, Divider, Drawer, Group, Paper, Stack, Tabs, Text, ThemeIcon } from '@mantine/core';
import { BookOpen, ExternalLink, Lightbulb } from 'lucide-react';

import { useGettingStartedProgress } from '../hooks/useGettingStarted';
import { useTranslation } from '../i18n/context';
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
  const { t } = useTranslation();
  return (
    <Drawer opened={opened} onClose={onClose} position="right" size="md" title={<Text fw={600}>{t('help.title')}</Text>}>
      <HelpTabs onNavigate={onClose} />
    </Drawer>
  );
}

/**
 * What the drawer holds. Apart from the drawer, whose content is drawn only
 * while it is open, so it can be drawn on its own.
 */
export function HelpTabs({ onNavigate }: { onNavigate: () => void }) {
  const { t } = useTranslation();
  return (
    <Tabs defaultValue="getting-started">
      <Tabs.List grow mb="lg">
        <Tabs.Tab value="getting-started">{t('start.title')}</Tabs.Tab>
        <Tabs.Tab value="glossary">{t('help.glossary')}</Tabs.Tab>
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
 * these requests are made when somebody opens Help and not on every page.
 */
function GettingStartedProgress({ onNavigate }: { onNavigate: () => void }) {
  const { progress, retry } = useGettingStartedProgress();
  return <GettingStartedTimeline progress={progress} onRetry={retry} onNavigate={onNavigate} />;
}
